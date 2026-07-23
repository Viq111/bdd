package bdd

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/viq111/bdd/internal/sqlite"
)

// Memory is a durable, workspace-scoped, named piece of knowledge that
// survives sessions and agent rotation. Unlike a card Note, a Memory is
// keyed and updatable rather than append-only and task-scoped.
type Memory struct {
	Key       string
	Body      string
	Prime     string // MemoryPrimeRequired or MemoryPrimeOptional; see Memory.Prime
	CreatedBy string
	UpdatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  int64
}

// Memory prime designations: whether `bdd prime` inlines a memory's full
// body (MemoryPrimeRequired) or only a key/first-line summary
// (MemoryPrimeOptional, the default). Unlike runes, memories have no
// "never" designation — --no-memories already covers that case.
const (
	MemoryPrimeRequired = RunePrimeRequired
	MemoryPrimeOptional = RunePrimeOptional
)

func validMemoryPrime(v string) bool {
	return v == MemoryPrimeRequired || v == MemoryPrimeOptional
}

// Remember is the input to (*DB).Remember. If Key is empty, Remember
// derives a readable slug plus a short content hash and reports the
// generated key on the returned Memory. Prime, when nil, leaves an
// existing memory's designation unchanged and defaults a new memory to
// MemoryPrimeOptional.
type Remember struct {
	Key   string
	Body  string
	Prime *string
	Actor string
}

// MemoryQuery is the input to (*DB).Memories. An empty Query lists every
// memory.
type MemoryQuery struct {
	Query string
}

// Remember atomically creates or updates a memory by key. When in.Key is
// empty (after trimming), a key is derived from in.Body: a readable slug
// followed by a short content hash, so repeated calls with identical
// untitled content converge on the same record instead of piling up
// duplicates. Every call increments Revision and writes an audit event
// (memory.create or memory.update) in the same transaction as the write.
func (db *DB) Remember(ctx context.Context, in Remember) (*Memory, error) {
	if err := db.checkMemoryReady(true); err != nil {
		return nil, err
	}
	if in.Prime != nil && !validMemoryPrime(*in.Prime) {
		return nil, &ValidationError{
			Fields: []string{"prime"},
			Detail: fmt.Sprintf("prime %q must be one of %q, %q", *in.Prime, MemoryPrimeRequired, MemoryPrimeOptional),
		}
	}

	key := strings.TrimSpace(in.Key)
	if key == "" {
		key = deriveMemoryKey(in.Body)
	}

	var out *Memory
	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		now := time.Now().UTC().Format(time.RFC3339Nano)

		var existingRevision int64
		var existingPrime string
		err = tx.QueryRowContext(ctx, `SELECT revision, prime FROM memories WHERE key = ?`, key).Scan(&existingRevision, &existingPrime)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			prime := MemoryPrimeOptional
			if in.Prime != nil {
				prime = *in.Prime
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO memories (key, body, prime, created_by, updated_by, created_at, updated_at, revision)
				VALUES (?, ?, ?, ?, ?, ?, ?, 1)`,
				key, in.Body, prime, in.Actor, in.Actor, now, now); err != nil {
				return err
			}
			if err := insertMemoryEvent(ctx, tx, key, 1, "memory.create", in.Actor, in.Body, now); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			prime := existingPrime
			if in.Prime != nil {
				prime = *in.Prime
			}
			newRevision := existingRevision + 1
			if _, err := tx.ExecContext(ctx, `
				UPDATE memories SET body = ?, prime = ?, updated_by = ?, updated_at = ?, revision = ? WHERE key = ?`,
				in.Body, prime, in.Actor, now, newRevision, key); err != nil {
				return err
			}
			if err := insertMemoryEvent(ctx, tx, key, newRevision, "memory.update", in.Actor, in.Body, now); err != nil {
				return err
			}
		}

		m, err := scanMemory(tx.QueryRowContext(ctx, `
			SELECT key, body, prime, created_by, updated_by, created_at, updated_at, revision
			FROM memories WHERE key = ?`, key))
		if err != nil {
			return err
		}
		out = m

		return tx.Commit()
	})
	if err != nil {
		return nil, fmt.Errorf("bdd: remember %s: %w", key, err)
	}
	return out, nil
}

// Memories returns memories matching q, searching case-insensitively across
// key and body. An empty (or whitespace-only) Query lists every memory.
// Results are ordered by updated_at descending, then key ascending.
func (db *DB) Memories(ctx context.Context, q MemoryQuery) ([]Memory, error) {
	if err := db.checkMemoryReady(false); err != nil {
		return nil, err
	}

	const columns = `key, body, prime, created_by, updated_by, created_at, updated_at, revision`

	query := strings.TrimSpace(q.Query)
	var rows *sql.Rows
	var err error
	if query == "" {
		rows, err = db.sql.QueryContext(ctx, `
			SELECT `+columns+` FROM memories
			ORDER BY updated_at DESC, key ASC`)
	} else {
		pattern := "%" + escapeLike(strings.ToLower(query)) + "%"
		rows, err = db.sql.QueryContext(ctx, `
			SELECT `+columns+` FROM memories
			WHERE LOWER(key) LIKE ? ESCAPE '\' OR LOWER(body) LIKE ? ESCAPE '\'
			ORDER BY updated_at DESC, key ASC`, pattern, pattern)
	}
	if err != nil {
		return nil, fmt.Errorf("bdd: memories: %w", err)
	}
	defer rows.Close()

	var out []Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, fmt.Errorf("bdd: memories: %w", err)
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bdd: memories: %w", err)
	}
	return out, nil
}

// Recall returns the full memory record for key, or ErrNotFound.
func (db *DB) Recall(ctx context.Context, key string) (*Memory, error) {
	if err := db.checkMemoryReady(false); err != nil {
		return nil, err
	}

	row := db.sql.QueryRowContext(ctx, `
		SELECT key, body, prime, created_by, updated_by, created_at, updated_at, revision
		FROM memories WHERE key = ?`, key)
	m, err := scanMemory(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("bdd: recall %s: %w", key, ErrNotFound)
		}
		return nil, fmt.Errorf("bdd: recall %s: %w", key, err)
	}
	return m, nil
}

// Forget deletes the memory identified by key and records a memory.delete
// audit event in the same transaction, or returns ErrNotFound if key does
// not exist.
func (db *DB) Forget(ctx context.Context, key string, actor string) error {
	if err := db.checkMemoryReady(true); err != nil {
		return err
	}

	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		var revision int64
		err = tx.QueryRowContext(ctx, `SELECT revision FROM memories WHERE key = ?`, key).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE key = ?`, key); err != nil {
			return err
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := insertMemoryEvent(ctx, tx, key, revision, "memory.delete", actor, "", now); err != nil {
			return err
		}

		return tx.Commit()
	})
	if err != nil {
		return fmt.Errorf("bdd: forget %s: %w", key, err)
	}
	return nil
}

// checkMemoryReady validates that db can serve a memory call: it must be
// open, at the current schema version, and (for write calls) not read-only.
func (db *DB) checkMemoryReady(write bool) error {
	db.mu.Lock()
	closed := db.closed
	tooOld := db.schemaTooOld
	db.mu.Unlock()

	if closed {
		return fmt.Errorf("bdd: database is closed: %w", ErrInvalidArgument)
	}
	if tooOld {
		return fmt.Errorf("bdd: %w", ErrSchemaTooOld)
	}
	if write && db.opts.ReadOnly {
		return fmt.Errorf("bdd: database is read-only: %w", ErrInvalidArgument)
	}
	return nil
}

// insertMemoryEvent records one audit event for a memory mutation. body is
// included in the payload for create/update actions and left empty for
// deletes, which record only the removed key and its final revision.
func insertMemoryEvent(ctx context.Context, tx *sql.Tx, key string, revision int64, action, actor, body, now string) error {
	payload := map[string]any{}
	if action != "memory.delete" {
		payload["body"] = body
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (subject_kind, subject_key, revision, action, actor, payload_json, created_at)
		VALUES ('memory', ?, ?, ?, ?, ?, ?)`,
		key, revision, action, actor, string(payloadJSON), now)
	return err
}

// memoryScanner is satisfied by both *sql.Row and *sql.Rows.
type memoryScanner interface {
	Scan(dest ...any) error
}

func scanMemory(s memoryScanner) (*Memory, error) {
	var (
		m                    Memory
		createdBy, updatedBy sql.NullString
		createdAt, updatedAt string
	)
	if err := s.Scan(&m.Key, &m.Body, &m.Prime, &createdBy, &updatedBy, &createdAt, &updatedAt, &m.Revision); err != nil {
		return nil, err
	}
	m.CreatedBy = createdBy.String
	m.UpdatedBy = updatedBy.String

	ct, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	ut, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}
	m.CreatedAt = ct
	m.UpdatedAt = ut

	return &m, nil
}

// deriveMemoryKey builds a readable key from body when the caller does not
// supply one: a slug of the body's leading alphanumeric content, followed by
// a short content hash so repeated Remember calls with identical body
// converge on the same key instead of creating duplicates.
func deriveMemoryKey(body string) string {
	hash := sha256.Sum256([]byte(body))
	suffix := hex.EncodeToString(hash[:])[:8]

	slug := slugify(body, 40)
	if slug == "" {
		return "memory-" + suffix
	}
	return slug + "-" + suffix
}

// slugify lowercases s, collapses runs of non-alphanumeric characters into a
// single hyphen, and truncates to maxLen.
func slugify(s string, maxLen int) string {
	var b strings.Builder
	prevDash := true // suppress a leading hyphen
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
		if b.Len() >= maxLen {
			break
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > maxLen {
		out = strings.TrimRight(out[:maxLen], "-")
	}
	return out
}

// escapeLike escapes SQL LIKE metacharacters in s so it can be safely
// wrapped in "%...%" and matched with ESCAPE '\'.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}
