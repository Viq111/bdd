package bdd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/viq111/bdd/internal/sqlite"
)

// Config keys that carry dedicated grammar and side effects on
// status_definitions/type_definitions, in addition to being stored
// verbatim in the config table.
const (
	ConfigKeyStatusCustom = "status.custom"
	ConfigKeyTypesCustom  = "types.custom"
)

// ConfigEntry is one key/value pair as returned by ConfigList.
type ConfigEntry struct {
	Key   string
	Value string
}

// StatusDefinition describes one status a workspace accepts, built-in or
// custom, as returned by Statuses.
type StatusDefinition struct {
	Name     Status
	Category StatusCategory
	BuiltIn  bool
}

// TypeDefinition describes one card type a workspace accepts, built-in or
// custom, as returned by Types.
type TypeDefinition struct {
	Name    CardType
	BuiltIn bool
}

// builtinCardTypes lists every built-in CardType, for reserved-word checks
// against types.custom.
var builtinCardTypes = []CardType{
	CardTypeBug, CardTypeTask, CardTypeFeature, CardTypeEpic, CardTypeDecision, CardTypeChore,
}

func isBuiltinStatus(name string) bool {
	_, ok := BuiltinStatusCategories[Status(name)]
	return ok
}

func isBuiltinType(name string) bool {
	for _, t := range builtinCardTypes {
		if string(t) == name {
			return true
		}
	}
	return false
}

var validStatusCategories = map[StatusCategory]bool{
	StatusCategoryActive: true,
	StatusCategoryWIP:    true,
	StatusCategoryDone:   true,
	StatusCategoryFrozen: true,
}

// configDefNamePattern constrains custom status/type names to the same
// lowercase, letter-led, snake_case-friendly grammar as the built-in names
// (open, in_progress, ...).
var configDefNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ConfigGet returns the value stored at key, or ErrNotFound if key has
// never been set (or was unset).
func (db *DB) ConfigGet(ctx context.Context, key string) (string, error) {
	if err := db.ready(); err != nil {
		return "", err
	}
	if key == "" {
		return "", fmt.Errorf("bdd: config get: key is required: %w", ErrInvalidArgument)
	}

	var value string
	err := db.sql.QueryRowContext(ctx, `SELECT value FROM config WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("bdd: config key %s: %w", key, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("bdd: config get: %w", err)
	}
	return value, nil
}

// ConfigList returns every configuration entry, ordered by key.
func (db *DB) ConfigList(ctx context.Context) ([]ConfigEntry, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}

	rows, err := db.sql.QueryContext(ctx, `SELECT key, value FROM config ORDER BY key ASC`)
	if err != nil {
		return nil, fmt.Errorf("bdd: config list: %w", err)
	}
	defer rows.Close()

	var out []ConfigEntry
	for rows.Next() {
		var e ConfigEntry
		if err := rows.Scan(&e.Key, &e.Value); err != nil {
			return nil, fmt.Errorf("bdd: config list: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ConfigSet atomically sets key to value. status.custom and types.custom
// carry dedicated grammar and side effects on status_definitions and
// type_definitions respectively (see parseStatusCustom, parseTypesCustom);
// every other key is stored verbatim with no extra validation.
func (db *DB) ConfigSet(ctx context.Context, key, value, actor string) error {
	if err := db.ready(); err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("bdd: config set: key is required: %w", ErrInvalidArgument)
	}

	return sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if err := applyConfigSideEffects(ctx, tx, key, value); err != nil {
			return err
		}

		now := formatTime(time.Now())
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO config (key, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
			key, value, now, actor); err != nil {
			return err
		}
		if err := writeConfigEvent(ctx, tx, key, "config.set", actor, value, now); err != nil {
			return err
		}
		return tx.Commit()
	})
}

// ConfigUnset atomically removes key. Unsetting status.custom or
// types.custom behaves like setting it to the empty list: it fails if any
// currently-defined custom status/type would still be used by a card after
// removal.
func (db *DB) ConfigUnset(ctx context.Context, key, actor string) error {
	if err := db.ready(); err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("bdd: config unset: key is required: %w", ErrInvalidArgument)
	}

	return sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if err := applyConfigSideEffects(ctx, tx, key, ""); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM config WHERE key = ?`, key); err != nil {
			return err
		}
		now := formatTime(time.Now())
		if err := writeConfigEvent(ctx, tx, key, "config.unset", actor, "", now); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func writeConfigEvent(ctx context.Context, tx *sql.Tx, key, action, actor, value, now string) error {
	payload, _ := json.Marshal(map[string]any{"key": key, "value": value})
	_, err := tx.ExecContext(ctx, `
		INSERT INTO events (subject_kind, subject_key, revision, action, actor, payload_json, created_at)
		VALUES ('config', ?, 0, ?, ?, ?, ?)`,
		key, action, actor, string(payload), now)
	return err
}

// applyConfigSideEffects synchronizes status_definitions/type_definitions
// with a status.custom or types.custom write, and is a no-op for every
// other key.
func applyConfigSideEffects(ctx context.Context, tx *sql.Tx, key, value string) error {
	switch key {
	case ConfigKeyStatusCustom:
		return applyStatusCustom(ctx, tx, value)
	case ConfigKeyTypesCustom:
		return applyTypesCustom(ctx, tx, value)
	default:
		return nil
	}
}

// customStatusEntry is one parsed name:category pair from status.custom.
type customStatusEntry struct {
	Name     string
	Category StatusCategory
}

// parseStatusCustom parses the status.custom grammar
// ("name:category,name:category,..."), validating name charset, category
// membership (active/wip/done/frozen), reserved built-in status names, and
// duplicates. An empty or whitespace-only value parses to a nil (empty)
// slice.
func parseStatusCustom(value string) ([]customStatusEntry, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	seen := make(map[string]bool)
	var out []customStatusEntry
	for _, tok := range strings.Split(value, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return nil, fmt.Errorf("bdd: status.custom: empty entry: %w", ErrInvalidArgument)
		}
		parts := strings.SplitN(tok, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("bdd: status.custom: %q must have the form name:category: %w", tok, ErrInvalidArgument)
		}
		name := strings.TrimSpace(parts[0])
		category := StatusCategory(strings.TrimSpace(parts[1]))

		if !configDefNamePattern.MatchString(name) {
			return nil, fmt.Errorf("bdd: status.custom: invalid status name %q: %w", name, ErrInvalidArgument)
		}
		if isBuiltinStatus(name) {
			return nil, fmt.Errorf("bdd: status.custom: %q is a reserved built-in status name: %w", name, ErrInvalidArgument)
		}
		if !validStatusCategories[category] {
			return nil, fmt.Errorf("bdd: status.custom: %q has invalid category %q (want active, wip, done, or frozen): %w", name, category, ErrInvalidArgument)
		}
		if seen[name] {
			return nil, fmt.Errorf("bdd: status.custom: duplicate status name %q: %w", name, ErrInvalidArgument)
		}
		seen[name] = true

		out = append(out, customStatusEntry{Name: name, Category: category})
	}
	return out, nil
}

// applyStatusCustom parses value and synchronizes status_definitions:
// custom statuses no longer present are deleted (failing the whole write if
// any card still uses one), and the rest are inserted or have their
// category updated (also failing if a card in use would have its status
// reclassified to a different category).
func applyStatusCustom(ctx context.Context, tx *sql.Tx, value string) error {
	entries, err := parseStatusCustom(value)
	if err != nil {
		return err
	}

	keep := make(map[string]bool, len(entries))
	for _, e := range entries {
		keep[e.Name] = true
	}

	currentCategories, err := customDefinitionCategories(ctx, tx, "status_definitions")
	if err != nil {
		return err
	}
	for name := range currentCategories {
		if keep[name] {
			continue
		}
		inUse, err := definitionInUse(ctx, tx, "cards", "status", name)
		if err != nil {
			return err
		}
		if inUse {
			return fmt.Errorf("bdd: status.custom: cannot remove status %q: still used by one or more cards: %w", name, ErrInvalidArgument)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM status_definitions WHERE name = ?`, name); err != nil {
			return err
		}
	}

	for _, e := range entries {
		if prevCategory, existed := currentCategories[e.Name]; existed && prevCategory != e.Category {
			inUse, err := definitionInUse(ctx, tx, "cards", "status", e.Name)
			if err != nil {
				return err
			}
			if inUse {
				return fmt.Errorf("bdd: status.custom: cannot change category of status %q: still used by one or more cards: %w", e.Name, ErrInvalidArgument)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO status_definitions (name, category, built_in) VALUES (?, ?, 0)
			ON CONFLICT(name) DO UPDATE SET category = excluded.category`,
			e.Name, string(e.Category)); err != nil {
			return err
		}
	}
	return nil
}

// parseTypesCustom parses the types.custom grammar ("name,name,..."),
// validating name charset, reserved built-in type names, and duplicates. An
// empty or whitespace-only value parses to a nil (empty) slice.
func parseTypesCustom(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	seen := make(map[string]bool)
	var out []string
	for _, tok := range strings.Split(value, ",") {
		name := strings.TrimSpace(tok)
		if name == "" {
			return nil, fmt.Errorf("bdd: types.custom: empty entry: %w", ErrInvalidArgument)
		}
		if !configDefNamePattern.MatchString(name) {
			return nil, fmt.Errorf("bdd: types.custom: invalid type name %q: %w", name, ErrInvalidArgument)
		}
		if isBuiltinType(name) {
			return nil, fmt.Errorf("bdd: types.custom: %q is a reserved built-in type name: %w", name, ErrInvalidArgument)
		}
		if seen[name] {
			return nil, fmt.Errorf("bdd: types.custom: duplicate type name %q: %w", name, ErrInvalidArgument)
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

// applyTypesCustom parses value and synchronizes type_definitions: custom
// types no longer present are deleted (failing the whole write if any card
// still uses one), and the rest are inserted if missing.
func applyTypesCustom(ctx context.Context, tx *sql.Tx, value string) error {
	names, err := parseTypesCustom(value)
	if err != nil {
		return err
	}

	keep := make(map[string]bool, len(names))
	for _, n := range names {
		keep[n] = true
	}

	current, err := customDefinitionNames(ctx, tx, "type_definitions")
	if err != nil {
		return err
	}
	for _, name := range current {
		if keep[name] {
			continue
		}
		inUse, err := definitionInUse(ctx, tx, "cards", "card_type", name)
		if err != nil {
			return err
		}
		if inUse {
			return fmt.Errorf("bdd: types.custom: cannot remove type %q: still used by one or more cards: %w", name, ErrInvalidArgument)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM type_definitions WHERE name = ?`, name); err != nil {
			return err
		}
	}

	for _, n := range names {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO type_definitions (name, built_in) VALUES (?, 0)
			ON CONFLICT(name) DO NOTHING`, n); err != nil {
			return err
		}
	}
	return nil
}

// customDefinitionNames returns the names of every non-built-in row in
// table (status_definitions or type_definitions).
func customDefinitionNames(ctx context.Context, tx *sql.Tx, table string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM `+table+` WHERE built_in = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// customDefinitionCategories returns the category of every non-built-in row
// in status_definitions, keyed by name.
func customDefinitionCategories(ctx context.Context, tx *sql.Tx, table string) (map[string]StatusCategory, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name, category FROM `+table+` WHERE built_in = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]StatusCategory)
	for rows.Next() {
		var name, category string
		if err := rows.Scan(&name, &category); err != nil {
			return nil, err
		}
		out[name] = StatusCategory(category)
	}
	return out, rows.Err()
}

// definitionInUse reports whether any row in table has column = value.
func definitionInUse(ctx context.Context, tx *sql.Tx, table, column, value string) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE `+column+` = ? LIMIT 1`, value).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Statuses returns every status the workspace accepts, built-in and
// custom, ordered built-in first, then by name.
func (db *DB) Statuses(ctx context.Context) ([]StatusDefinition, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}

	rows, err := db.sql.QueryContext(ctx, `SELECT name, category, built_in FROM status_definitions ORDER BY built_in DESC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("bdd: statuses: %w", err)
	}
	defer rows.Close()

	var out []StatusDefinition
	for rows.Next() {
		var name, category string
		var builtIn int
		if err := rows.Scan(&name, &category, &builtIn); err != nil {
			return nil, fmt.Errorf("bdd: statuses: %w", err)
		}
		out = append(out, StatusDefinition{Name: Status(name), Category: StatusCategory(category), BuiltIn: builtIn != 0})
	}
	return out, rows.Err()
}

// Types returns every card type the workspace accepts, built-in and
// custom, ordered built-in first, then by name.
func (db *DB) Types(ctx context.Context) ([]TypeDefinition, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}

	rows, err := db.sql.QueryContext(ctx, `SELECT name, built_in FROM type_definitions ORDER BY built_in DESC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("bdd: types: %w", err)
	}
	defer rows.Close()

	var out []TypeDefinition
	for rows.Next() {
		var name string
		var builtIn int
		if err := rows.Scan(&name, &builtIn); err != nil {
			return nil, fmt.Errorf("bdd: types: %w", err)
		}
		out = append(out, TypeDefinition{Name: CardType(name), BuiltIn: builtIn != 0})
	}
	return out, rows.Err()
}
