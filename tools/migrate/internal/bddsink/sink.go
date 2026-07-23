// Package bddsink applies a canonical migration plan to a version-pinned bdd
// SQLite database.  It is deliberately the only migration package that uses
// bdd's storage schema directly.
package bddsink

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/viq111/bdd"
	"github.com/viq111/bdd/internal/sqlite"
	"github.com/viq111/bdd/tools/migrate/internal/model"
)

const actor = "bdd-migration"

// Apply initializes destination through bdd.Init when it does not exist and
// then writes the complete plan in one SQLite transaction.  A new database is
// initialized at a sibling path and only published after public API checks.
func Apply(ctx context.Context, destination, prefix string, plan model.Plan) error {
	_, err := ApplyWithWarnings(ctx, destination, prefix, plan)
	return err
}

// ApplyWithWarnings applies a plan and reports destination collisions that
// were deliberately skipped.  Mapping warnings remain owned by mapping; these
// warnings require inspecting the destination and are therefore discovered by
// the sink.
func ApplyWithWarnings(ctx context.Context, destination, prefix string, plan model.Plan) (warnings []model.Warning, err error) {
	if destination == "" || prefix == "" {
		return nil, fmt.Errorf("bdd migration sink: destination and prefix are required")
	}
	plan.Canonicalize()
	_, statErr := os.Stat(destination)
	newDB := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !newDB {
		return nil, fmt.Errorf("bdd migration sink: stat destination: %w", statErr)
	}
	path := destination
	if newDB {
		path = filepath.Join(filepath.Dir(destination), "."+filepath.Base(destination)+".migration-tmp")
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		defer func() {
			if err != nil {
				for _, suffix := range []string{"", "-wal", "-shm"} {
					_ = os.Remove(path + suffix)
				}
			}
		}()
		db, initErr := bdd.Init(ctx, bdd.InitOptions{DBPath: path, Prefix: prefix})
		if initErr != nil {
			return nil, fmt.Errorf("bdd migration sink: initialize destination: %w", initErr)
		}
		if closeErr := db.Close(); closeErr != nil {
			return nil, closeErr
		}
	}
	var effective model.Plan
	if effective, warnings, err = apply(ctx, path, plan); err != nil {
		return nil, err
	}
	if err = verifyPublic(ctx, path, effective); err != nil {
		if newDB {
			return nil, err
		}
		return nil, fmt.Errorf("bdd migration sink: transaction committed but verification failed: %w", err)
	}
	if newDB {
		if err = os.Rename(path, destination); err != nil {
			return nil, fmt.Errorf("bdd migration sink: publish destination: %w", err)
		}
	}
	return warnings, nil
}

func apply(ctx context.Context, path string, plan model.Plan) (model.Plan, []model.Warning, error) {
	db, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot, SkipJournalMode: true})
	if err != nil {
		return model.Plan{}, nil, err
	}
	defer db.Close()
	if err := checkSchema(ctx, db); err != nil {
		return model.Plan{}, nil, err
	}
	var effective model.Plan
	var warnings []model.Warning
	err = sqlite.Retry(ctx, func() error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var prepErr error
		effective, warnings, prepErr = prepare(ctx, tx, plan)
		if prepErr != nil {
			return prepErr
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for _, v := range effective.Workspace.Statuses {
			if err := definition(ctx, tx, "status_definitions", v.Name, v.Category); err != nil {
				return err
			}
		}
		for _, v := range effective.Workspace.Types {
			if err := definition(ctx, tx, "type_definitions", v.Name, ""); err != nil {
				return err
			}
		}
		for _, v := range effective.Cards {
			if err := card(ctx, tx, v, now); err != nil {
				return err
			}
		}
		for _, v := range effective.Notes {
			if err := note(ctx, tx, v, now); err != nil {
				return err
			}
		}
		if err := reconcileEdges(ctx, tx, effective.Edges, now); err != nil {
			return err
		}
		for _, v := range effective.Runes {
			if err := rune(ctx, tx, v, now); err != nil {
				return err
			}
		}
		for _, v := range effective.Memories {
			if err := memory(ctx, tx, v, now); err != nil {
				return err
			}
		}
		if err := verifyTx(ctx, tx, effective); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return model.Plan{}, nil, err
	}
	return effective, warnings, nil
}

func definition(ctx context.Context, tx *sql.Tx, table, name, category string) error {
	if table == "status_definitions" {
		var old string
		err := tx.QueryRowContext(ctx, `SELECT category FROM status_definitions WHERE name = ?`, name).Scan(&old)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, `INSERT INTO status_definitions (name, category, built_in) VALUES (?, ?, 0)`, name, category)
			return err
		}
		if err != nil {
			return err
		}
		if old != category {
			return fmt.Errorf("bdd migration sink: incompatible status %q", name)
		}
		return nil
	}
	var n string
	err := tx.QueryRowContext(ctx, `SELECT name FROM type_definitions WHERE name = ?`, name).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO type_definitions (name, built_in) VALUES (?, 0)`, name)
		return err
	}
	return err
}

// prepare filters destination-owned collisions before any write and carries
// forward destination-only fields that Beads cannot represent.  It is called
// inside the import transaction so ownership decisions and writes see one
// consistent destination snapshot.
func prepare(ctx context.Context, tx *sql.Tx, plan model.Plan) (model.Plan, []model.Warning, error) {
	effective := plan
	effective.Cards = nil
	effective.Runes = nil
	effective.Notes = nil
	effective.Edges = nil
	acceptedCards := make(map[string]bool, len(plan.Cards))
	var warnings []model.Warning
	for _, source := range plan.Cards {
		var worktree string
		var payload sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT c.worktree,e.payload_json FROM cards c LEFT JOIN events e ON e.subject_kind='card' AND e.subject_key=c.id AND e.action='migration.card' WHERE c.id=?`, source.ID).Scan(&worktree, &payload)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			acceptedCards[source.ID] = true
			effective.Cards = append(effective.Cards, source)
		case err != nil:
			return model.Plan{}, nil, err
		case !cardProvenance(payload.String, source.ID):
			warnings = append(warnings, model.Warning{SourceID: source.ID, Reasons: []string{"destination-owned card ID collision; skipped record"}})
		default:
			// An empty source worktree means there was no recognized Beads
			// value. Carry it for exact verification, while retaining the
			// canonical source hash so it remains destination-owned.
			if source.Worktree == "" {
				source.Worktree = worktree
			}
			acceptedCards[source.ID] = true
			effective.Cards = append(effective.Cards, source)
		}
	}
	for _, source := range plan.Notes {
		if acceptedCards[source.CardID] {
			note, err := effectiveNote(ctx, tx, source)
			if err != nil {
				return model.Plan{}, nil, err
			}
			effective.Notes = append(effective.Notes, note)
		}
	}
	for _, source := range plan.Edges {
		if acceptedCards[source.ParentID] && acceptedCards[source.ChildID] {
			effective.Edges = append(effective.Edges, source)
		}
	}
	for _, source := range plan.Runes {
		legacyID := source.Metadata["legacy_bd_id"]
		var existingKey, metadata string
		err := tx.QueryRowContext(ctx, `SELECT key,metadata_json FROM runes WHERE key=?`, source.Key).Scan(&existingKey, &metadata)
		if errors.Is(err, sql.ErrNoRows) {
			// A role rune key is derived from its source title, which can change.
			// The legacy ID is stable, so use it to find and update the already
			// imported rune rather than leaving the old key stale and creating a
			// second rune at the newly-derived key.
			rows, queryErr := tx.QueryContext(ctx, `SELECT key,metadata_json FROM runes`)
			if queryErr != nil {
				return model.Plan{}, nil, queryErr
			}
			for rows.Next() {
				var key, candidate string
				if scanErr := rows.Scan(&key, &candidate); scanErr != nil {
					rows.Close()
					return model.Plan{}, nil, scanErr
				}
				var existing map[string]string
				if json.Unmarshal([]byte(candidate), &existing) == nil && legacyID != "" && existing["legacy_bd_id"] == legacyID {
					existingKey, metadata = key, candidate
					break
				}
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				rows.Close()
				return model.Plan{}, nil, rowsErr
			}
			if closeErr := rows.Close(); closeErr != nil {
				return model.Plan{}, nil, closeErr
			}
			if existingKey == "" {
				effective.Runes = append(effective.Runes, source)
				continue
			}
			err = nil
		}
		if err != nil {
			return model.Plan{}, nil, err
		}
		var existing map[string]string
		if json.Unmarshal([]byte(metadata), &existing) != nil || existing["legacy_bd_id"] != legacyID {
			warnings = append(warnings, model.Warning{SourceID: legacyID, Reasons: []string{"destination-owned rune key collision; skipped record"}})
			continue
		}
		source.Key = existingKey
		effective.Runes = append(effective.Runes, source)
	}
	return effective, canonicalWarnings(warnings), nil
}

func cardProvenance(payload, id string) bool {
	if payload == "" {
		return false
	}
	var value struct {
		SourceSystem string `json:"source_system"`
		SourceKind   string `json:"source_kind"`
		SourceID     string `json:"source_id"`
	}
	return json.Unmarshal([]byte(payload), &value) == nil && value.SourceSystem == "beads" && value.SourceKind == "issue" && value.SourceID == id
}

func canonicalWarnings(in []model.Warning) []model.Warning {
	if len(in) == 0 {
		return nil
	}
	p := model.Plan{Warnings: in}
	p.Canonicalize()
	return p.Warnings
}

func card(ctx context.Context, tx *sql.Tx, v model.CardPlan, now string) error {
	created, updated := timestamp(v.CreatedAt, now), timestamp(v.UpdatedAt, timestamp(v.CreatedAt, now))
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM cards WHERE id = ?`, v.ID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO cards (id,title,worktree,description,reproduction,design,acceptance,status,priority,card_type,external_ref,assignee,created_by,owner,created_at,updated_at,started_at,closed_at,defer_until,revision) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,NULL,?,?,1)`, v.ID, v.Title, v.Worktree, v.Description, v.Reproduction, v.Design, v.Acceptance, v.Status, v.Priority, v.Type, v.ExternalRef, v.Assignee, v.Creator, v.Owner, created, updated, timeValue(v.ClosedAt), timeValue(v.DeferUntil))
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		var oldHash string
		err = tx.QueryRowContext(ctx, `SELECT json_extract(payload_json, '$.hash') FROM events WHERE subject_kind='card' AND subject_key=? AND action='migration.card'`, v.ID).Scan(&oldHash)
		if err != nil {
			return err
		}
		matches, err := cardProjectionMatches(ctx, tx, v)
		if err != nil {
			return err
		}
		if oldHash == v.Hash && matches {
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE cards SET title=?,worktree=CASE WHEN ?<>'' THEN ? ELSE worktree END,description=?,reproduction=?,design=?,acceptance=?,status=?,priority=?,card_type=?,external_ref=?,assignee=?,created_by=?,owner=?,created_at=CASE WHEN ? THEN ? ELSE created_at END,updated_at=CASE WHEN ? THEN ? ELSE updated_at END,closed_at=?,defer_until=?,revision=revision+1 WHERE id=?`, v.Title, v.Worktree, v.Worktree, v.Description, v.Reproduction, v.Design, v.Acceptance, v.Status, v.Priority, v.Type, v.ExternalRef, v.Assignee, v.Creator, v.Owner, v.CreatedAt != nil, created, v.UpdatedAt != nil, updated, timeValue(v.ClosedAt), timeValue(v.DeferUntil), v.ID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM labels WHERE card_id=?`, v.ID); err != nil {
			return err
		}
	}
	for _, label := range v.Labels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO labels (card_id,label) VALUES (?,?) ON CONFLICT(card_id,label) DO NOTHING`, v.ID, label); err != nil {
			return err
		}
	}
	return provenance(ctx, tx, "card", v.ID, 1, "migration.card", map[string]any{"source_system": "beads", "source_kind": "issue", "source_id": v.ID, "hash_version": v.HashVersion, "hash": v.Hash}, now)
}

// cardProjectionMatches detects a native edit to a mapped field even when
// the source hash is unchanged. Destination-only worktree data and omitted
// created/updated timestamps are normalized away before hashing, so they do
// not turn an otherwise identical rerun into a write. Closed and deferred
// timestamps remain source-owned even when NULL.
func cardProjectionMatches(ctx context.Context, tx *sql.Tx, want model.CardPlan) (bool, error) {
	var got model.CardPlan
	var created, updated string
	var closed, deferUntil sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id,title,worktree,description,reproduction,design,acceptance,status,priority,card_type,external_ref,assignee,created_by,owner,created_at,updated_at,closed_at,defer_until FROM cards WHERE id=?`, want.ID).Scan(&got.ID, &got.Title, &got.Worktree, &got.Description, &got.Reproduction, &got.Design, &got.Acceptance, &got.Status, &got.Priority, &got.Type, &got.ExternalRef, &got.Assignee, &got.Creator, &got.Owner, &created, &updated, &closed, &deferUntil)
	if err != nil {
		return false, err
	}
	if sourceOmittedWorktree(want) {
		got.Worktree = ""
	}
	rows, err := tx.QueryContext(ctx, `SELECT label FROM labels WHERE card_id=? ORDER BY label`, want.ID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return false, err
		}
		got.Labels = append(got.Labels, label)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	// Source-provided created/updated timestamps participate in the projection.
	// Read those values from storage so native drift is reconciled even when
	// the source hash itself is unchanged.
	if want.CreatedAt != nil {
		got.CreatedAt, err = parseTimestamp(created)
		if err != nil {
			return false, err
		}
	}
	if want.UpdatedAt != nil {
		got.UpdatedAt, err = parseTimestamp(updated)
		if err != nil {
			return false, err
		}
	}
	// ClosedAt and DeferUntil are source-owned options: a source NULL must
	// therefore compare unequal to a non-NULL destination value and clear it.
	if closed.Valid {
		got.ClosedAt, err = parseTimestamp(closed.String)
		if err != nil {
			return false, err
		}
	}
	if deferUntil.Valid {
		got.DeferUntil, err = parseTimestamp(deferUntil.String)
		if err != nil {
			return false, err
		}
	}
	p := model.Plan{Cards: []model.CardPlan{got}}
	p.Canonicalize()
	return p.Cards[0].Hash == want.Hash, nil
}

func parseTimestamp(value string) (*time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("bdd migration sink: parse card timestamp %q: %w", value, err)
	}
	return &parsed, nil
}

// sourceOmittedWorktree distinguishes a source-empty worktree from the
// destination value carried into the effective plan by prepare. The latter is
// needed for verification, but the provenance hash must continue to represent
// the source projection rather than the preserved native value.
func sourceOmittedWorktree(v model.CardPlan) bool {
	withoutWorktree := v
	withoutWorktree.Worktree = ""
	p := model.Plan{Cards: []model.CardPlan{withoutWorktree}}
	p.Canonicalize()
	return p.Cards[0].Hash == v.Hash
}

type noteProvenance struct {
	SourceSystem string `json:"source_system"`
	SourceKind   string `json:"source_kind"`
	SourceID     string `json:"source_id"`
	SourceKey    string `json:"source_key"`
	NoteID       int64  `json:"note_id"`
	HashVersion  int    `json:"hash_version"`
	Hash         string `json:"hash"`
}

// effectiveNote preserves the first projection recorded for a source key.
// Notes are append-only in the destination, so a structured source comment
// whose stable key is later edited cannot replace its imported history or add
// a duplicate.  Verification therefore uses that original projection too.
func effectiveNote(ctx context.Context, tx *sql.Tx, source model.NotePlan) (model.NotePlan, error) {
	var payload string
	err := tx.QueryRowContext(ctx, `SELECT payload_json FROM events WHERE subject_kind='note' AND subject_key=? AND action='migration.note'`, source.SourceKey).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return source, nil
	}
	if err != nil {
		return model.NotePlan{}, err
	}
	var existing noteProvenance
	if err := json.Unmarshal([]byte(payload), &existing); err != nil {
		return model.NotePlan{}, err
	}
	if existing.Hash == source.Hash && existing.HashVersion == source.HashVersion {
		return source, nil
	}
	if existing.SourceSystem != "beads" || existing.SourceKind != source.SourceKind || existing.SourceID != source.SourceID || existing.SourceKey != source.SourceKey {
		// Keep the incoming projection so verification retains its existing
		// provenance-mismatch failure for a source-key collision.
		return source, nil
	}

	var note model.NotePlan
	var author sql.NullString
	var created string
	if err := tx.QueryRowContext(ctx, `SELECT card_id,author,body,created_at FROM notes WHERE id=?`, existing.NoteID).Scan(&note.CardID, &author, &note.Body, &created); err != nil {
		return model.NotePlan{}, err
	}
	createdAt, err := parseTimestamp(created)
	if err != nil {
		return model.NotePlan{}, err
	}
	note.SourceKey = source.SourceKey
	note.SourceKind = existing.SourceKind
	note.SourceID = existing.SourceID
	note.Author = author.String
	note.CreatedAt = createdAt
	note.HashVersion = existing.HashVersion
	note.Hash = existing.Hash
	return note, nil
}

func note(ctx context.Context, tx *sql.Tx, v model.NotePlan, now string) error {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM events WHERE subject_kind='note' AND subject_key=? AND action='migration.note' LIMIT 1`, v.SourceKey).Scan(&n)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO notes (card_id,author,body,created_at) VALUES (?,?,?,?)`, v.CardID, nullString(v.Author), v.Body, timestamp(v.CreatedAt, now))
	if err != nil {
		return err
	}
	noteID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	return provenance(ctx, tx, "note", v.SourceKey, 1, "migration.note", map[string]any{"source_system": "beads", "source_kind": v.SourceKind, "source_id": v.SourceID, "source_key": v.SourceKey, "note_id": noteID, "hash_version": v.HashVersion, "hash": v.Hash}, now)
}
func reconcileEdges(ctx context.Context, tx *sql.Tx, want []model.EdgePlan, now string) error {
	wanted := make(map[string]bool, len(want))
	for _, v := range want {
		wanted[v.ParentID+"\x00"+v.ChildID] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO card_edges (parent_id,child_id,created_at,created_by) VALUES (?,?,?,?) ON CONFLICT(parent_id,child_id) DO NOTHING`, v.ParentID, v.ChildID, now, actor); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT e.parent_id,e.child_id FROM card_edges e WHERE EXISTS (SELECT 1 FROM events p WHERE p.subject_kind='card' AND p.subject_key=e.parent_id AND p.action='migration.card') AND EXISTS (SELECT 1 FROM events c WHERE c.subject_kind='card' AND c.subject_key=e.child_id AND c.action='migration.card')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var remove []model.EdgePlan
	for rows.Next() {
		var edge model.EdgePlan
		if err := rows.Scan(&edge.ParentID, &edge.ChildID); err != nil {
			return err
		}
		if !wanted[edge.ParentID+"\x00"+edge.ChildID] {
			remove = append(remove, edge)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, edge := range remove {
		if _, err := tx.ExecContext(ctx, `DELETE FROM card_edges WHERE parent_id=? AND child_id=?`, edge.ParentID, edge.ChildID); err != nil {
			return err
		}
	}
	return nil
}
func rune(ctx context.Context, tx *sql.Tx, v model.RunePlan, now string) error {
	metadata, err := json.Marshal(v.Metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO runes (key,kind,title,body,metadata_json,enabled,protected,created_by,updated_by,created_at,updated_at,revision) VALUES (?,?,?,?,?,?,?,?,?,?,?,1) ON CONFLICT(key) DO UPDATE SET title=excluded.title,body=excluded.body,metadata_json=excluded.metadata_json,enabled=excluded.enabled,protected=excluded.protected,updated_by=excluded.updated_by,updated_at=excluded.updated_at,revision=runes.revision+1 WHERE runes.kind<>excluded.kind OR runes.title<>excluded.title OR runes.body<>excluded.body OR runes.metadata_json<>excluded.metadata_json OR runes.enabled<>excluded.enabled OR runes.protected<>excluded.protected`, v.Key, v.Kind, v.Title, v.Body, string(metadata), boolInt(v.Enabled), boolInt(v.Protected), actor, actor, now, now)
	return err
}
func memory(ctx context.Context, tx *sql.Tx, v model.MemoryPlan, now string) error {
	created := timestamp(v.CreatedAt, now)
	_, err := tx.ExecContext(ctx, `INSERT INTO memories (key,body,created_by,updated_by,created_at,updated_at,revision) VALUES (?,?,?,?,?,?,1) ON CONFLICT(key) DO UPDATE SET body=excluded.body,updated_by=excluded.updated_by,updated_at=excluded.updated_at,revision=memories.revision+1 WHERE memories.body<>excluded.body`, v.Key, v.Body, v.Actor, v.Actor, created, now)
	return err
}
func provenance(ctx context.Context, tx *sql.Tx, kind, key string, revision int64, action string, payload any, now string) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO events (subject_kind,subject_key,revision,action,actor,payload_json,created_at) SELECT ?,?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM events WHERE subject_kind=? AND subject_key=? AND action=?)`, kind, key, revision, action, actor, string(b), now, kind, key, action)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE events SET revision=?, payload_json=? WHERE subject_kind=? AND subject_key=? AND action=? AND payload_json<>?`, revision, string(b), kind, key, action, string(b))
	return err
}
func timestamp(v *time.Time, fallback string) string {
	if v == nil {
		return fallback
	}
	return v.UTC().Format(time.RFC3339Nano)
}
func timeValue(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.UTC().Format(time.RFC3339Nano)
}
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
