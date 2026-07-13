package fixture

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/viq111/bdd/internal/schema"

	_ "modernc.org/sqlite"
)

// Options configures Generate.
type Options struct {
	// Path is the SQLite file to create. Generate fails if it already
	// exists.
	Path string
	// Cards is the number of cards to generate. Must be >= 1.
	Cards int
	// Seed makes generation deterministic: the same Seed and Cards always
	// produce byte-for-byte identical data.
	Seed int64
	// IDPrefix is the workspace ID prefix used to build card IDs
	// ("<prefix>-<suffix>").
	IDPrefix string
}

// Manifest describes a generated fixture: enough for a benchmark harness or
// test to exercise realistic commands without re-reading the database.
type Manifest struct {
	Path        string    `json:"path"`
	Seed        int64     `json:"seed"`
	CardCount   int       `json:"card_count"`
	GeneratedAt time.Time `json:"generated_at"`

	// ShowID is a card ID with notes, labels, and edges, safe to pass to
	// `bdd show`.
	ShowID string `json:"show_id"`
	// ClaimID is a StatusOpen card ID with no assignee, safe to pass to
	// `bdd update --claim`.
	ClaimID string `json:"claim_id"`
	// SearchQuery is a token that appears in a handful of card
	// descriptions, suitable for `bdd search`.
	SearchQuery string `json:"search_query"`
}

var labelPool = []string{
	"runtime:claude", "runtime:codex", "area:cli", "area:storage",
	"area:schema", "area:docs", "priority:high", "needs-repro",
	"good-first-issue", "flaky", "regression", "perf", "security",
	"ux", "api", "breaking-change", "tech-debt", "customer-reported",
	"blocked-external", "human", "needs-design", "ci", "release-blocker",
	"discussion", "wontfix-candidate",
}

var actorPool = []string{
	"alice", "bob", "carol", "dave", "erin", "frank", "grace", "heidi",
}

var searchWords = []string{
	"latency", "regression", "timeout", "migration", "workspace",
	"benchmark", "schema", "cache", "concurrency", "retry",
}

type weighted[T any] struct {
	value  T
	weight int
}

func pick[T any](rng *rand.Rand, items []weighted[T]) T {
	total := 0
	for _, it := range items {
		total += it.weight
	}
	r := rng.Intn(total)
	for _, it := range items {
		if r < it.weight {
			return it.value
		}
		r -= it.weight
	}
	return items[len(items)-1].value
}

// Generate writes a new bdd-shaped SQLite database at opts.Path containing
// opts.Cards cards with a realistic distribution of types, statuses,
// priorities, labels, parent/child edges, and notes, and returns a Manifest
// describing it. Generation is deterministic in opts.Seed and opts.Cards.
func Generate(opts Options) (*Manifest, error) {
	if opts.Cards < 1 {
		return nil, fmt.Errorf("fixture: Cards must be >= 1, got %d", opts.Cards)
	}
	if opts.IDPrefix == "" {
		opts.IDPrefix = "bdd"
	}
	if _, err := os.Stat(opts.Path); err == nil {
		return nil, fmt.Errorf("fixture: %s already exists", opts.Path)
	}

	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

	db, err := sql.Open("sqlite", opts.Path)
	if err != nil {
		return nil, fmt.Errorf("fixture: open: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("fixture: enable WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		return nil, fmt.Errorf("fixture: enable foreign_keys: %w", err)
	}
	// Apply the same embedded migrations the real bdd storage layer uses, so
	// the fixture's schema (tables, columns, seeded statuses/types) can never
	// drift from what Open expects. schema_versions and PRAGMA user_version
	// are recorded here (rather than via schema.Upgrade, which stamps
	// applied_at with the wall clock) so fixtures stay byte-for-byte
	// deterministic in opts.Seed.
	for _, m := range schema.Migrations() {
		if _, err := db.ExecContext(context.Background(), m.SQL); err != nil {
			return nil, fmt.Errorf("fixture: applying migration %d (%s): %w", m.Version, m.Name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_versions (version, applied_at) VALUES (?, ?)`, m.Version, rfc3339(now)); err != nil {
			return nil, fmt.Errorf("fixture: recording migration %d: %w", m.Version, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schema.CurrentVersion())); err != nil {
		return nil, fmt.Errorf("fixture: setting user_version: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("fixture: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO workspace(singleton, prefix, created_at) VALUES (1, ?, ?)`, opts.IDPrefix, rfc3339(now)); err != nil {
		return nil, err
	}

	rng := rand.New(rand.NewSource(opts.Seed))

	typeWeights := []weighted[string]{
		{"task", 40}, {"bug", 25}, {"feature", 15}, {"chore", 10}, {"epic", 5}, {"decision", 5},
	}
	statusWeights := []weighted[string]{
		{"open", 35}, {"in_progress", 15}, {"awaiting_review", 5}, {"blocked", 10}, {"deferred", 5}, {"closed", 30},
	}
	priorityWeights := []weighted[int]{
		{0, 10}, {1, 30}, {2, 50}, {3, 10},
	}

	insertCard, err := tx.Prepare(`
		INSERT INTO cards (
			id, title, worktree, description, reproduction, design,
			acceptance, status, priority, card_type, external_ref, assignee,
			created_by, dispatchable, created_at, updated_at, started_at,
			closed_at, defer_until, revision
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer insertCard.Close()

	insertLabel, err := tx.Prepare(`INSERT INTO labels(card_id, label) VALUES (?, ?)`)
	if err != nil {
		return nil, err
	}
	defer insertLabel.Close()

	insertEdge, err := tx.Prepare(`INSERT INTO card_edges(parent_id, child_id, created_at) VALUES (?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer insertEdge.Close()

	insertNote, err := tx.Prepare(`INSERT INTO notes(card_id, body, author, created_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer insertNote.Close()

	ids := make([]string, opts.Cards)
	seen := make(map[string]bool, opts.Cards)
	genID := func() string {
		for {
			id := fmt.Sprintf("%s-%06x", opts.IDPrefix, rng.Int31n(1<<24))
			if !seen[id] {
				seen[id] = true
				return id
			}
		}
	}

	var manifest Manifest
	manifest.Path = opts.Path
	manifest.Seed = opts.Seed
	manifest.CardCount = opts.Cards
	manifest.GeneratedAt = now

	for i := 0; i < opts.Cards; i++ {
		id := genID()
		ids[i] = id

		typ := pick(rng, typeWeights)
		status := pick(rng, statusWeights)
		priority := pick(rng, priorityWeights)
		createdAt := now.Add(-time.Duration(rng.Intn(365*24)) * time.Hour)
		updatedAt := createdAt.Add(time.Duration(rng.Intn(72)) * time.Hour)

		var startedAt, closedAt, deferUntil any
		assignee := ""
		dispatchable := 1

		switch status {
		case "in_progress", "awaiting_review":
			startedAt = rfc3339(updatedAt)
			assignee = actorPool[rng.Intn(len(actorPool))]
		case "closed":
			startedAt = rfc3339(updatedAt.Add(-time.Hour))
			closedAt = rfc3339(updatedAt)
		case "deferred":
			deferUntil = rfc3339(updatedAt.Add(30 * 24 * time.Hour))
		case "blocked":
			dispatchable = 0
		}

		word := searchWords[rng.Intn(len(searchWords))]
		description := fmt.Sprintf("Card %d covers a %s related to %s in the bdd workspace.", i, typ, word)
		reproduction, design, acceptance, externalRef, worktree := "", "", "", "", ""
		switch typ {
		case "bug":
			reproduction = "1. Run bdd. 2. Observe unexpected output."
			acceptance = "The reported defect no longer reproduces."
		case "task", "feature", "epic":
			acceptance = "The described behavior is implemented and covered by tests."
		case "decision":
			design = "Considered alternatives and settled on the documented approach."
		}
		if rng.Intn(4) == 0 {
			externalRef = fmt.Sprintf("EXT-%d", rng.Intn(9999))
		}
		if status == "in_progress" || status == "awaiting_review" {
			worktree = fmt.Sprintf(".worktrees/%s", id)
		}

		title := fmt.Sprintf("%s #%d: %s", capitalize(typ), i, word)

		_, err = insertCard.Exec(
			id, title, worktree, description, reproduction, design,
			acceptance, status, priority, typ, externalRef, assignee,
			actorPool[rng.Intn(len(actorPool))], dispatchable, rfc3339(createdAt),
			rfc3339(updatedAt), startedAt, closedAt, deferUntil, 1,
		)
		if err != nil {
			return nil, fmt.Errorf("fixture: insert card %s: %w", id, err)
		}

		numLabels := pick(rng, []weighted[int]{{0, 20}, {1, 40}, {2, 25}, {3, 10}, {4, 5}})
		usedLabels := map[string]bool{}
		for l := 0; l < numLabels; l++ {
			label := labelPool[rng.Intn(len(labelPool))]
			if usedLabels[label] {
				continue
			}
			usedLabels[label] = true
			if _, err := insertLabel.Exec(id, label); err != nil {
				return nil, err
			}
		}

		// Parent edges only reference earlier cards, so the graph is
		// guaranteed acyclic.
		if i > 100 && rng.Intn(100) < 15 {
			numParents := 1 + rng.Intn(2)
			usedParents := map[string]bool{}
			for p := 0; p < numParents; p++ {
				parent := ids[rng.Intn(i)]
				if usedParents[parent] {
					continue
				}
				usedParents[parent] = true
				if _, err := insertEdge.Exec(parent, id, rfc3339(createdAt)); err != nil {
					return nil, err
				}
			}
		}

		numNotes := pick(rng, []weighted[int]{{0, 30}, {1, 30}, {2, 20}, {3, 12}, {4, 5}, {5, 3}})
		for n := 0; n < numNotes; n++ {
			noteTime := createdAt.Add(time.Duration(n+1) * time.Hour)
			body := fmt.Sprintf("Note %d on %s: progress update regarding %s.", n, id, word)
			if _, err := insertNote.Exec(id, body, actorPool[rng.Intn(len(actorPool))], rfc3339(noteTime)); err != nil {
				return nil, err
			}
		}

		if manifest.ShowID == "" && numNotes > 0 && numLabels > 0 {
			manifest.ShowID = id
		}
		if manifest.ClaimID == "" && status == "open" && assignee == "" {
			manifest.ClaimID = id
		}
	}

	manifest.SearchQuery = searchWords[0]
	if manifest.ShowID == "" {
		manifest.ShowID = ids[0]
	}
	if manifest.ClaimID == "" {
		manifest.ClaimID = ids[len(ids)-1]
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("fixture: commit: %w", err)
	}

	// Fold the WAL back into the main file and drop journal_mode back to
	// DELETE so the fixture is a single, self-contained file with no -wal
	// or -shm sidecars to ship or clean up.
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		return nil, fmt.Errorf("fixture: checkpoint: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=DELETE;"); err != nil {
		return nil, fmt.Errorf("fixture: reset journal mode: %w", err)
	}

	return &manifest, nil
}

func rfc3339(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000Z07:00")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-32) + s[1:]
}
