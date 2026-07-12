// Package fixture generates deterministic bdd workspace SQLite databases for
// benchmarking and QA verification, independent of the (not yet implemented)
// bdd storage layer.
//
// The schema below is QA's best-effort reconstruction of plan section 18
// (schema_versions, workspace, status_definitions, type_definitions, cards,
// labels, card_edges, notes, memories, runes, events, config) from the
// public API contract frozen in bdd-4s2w and the table list described in
// bdd-8urh. No authoritative plan document exists in-repo; once bdd-8urh
// lands, this schema should be reconciled against the real migrations so
// fixtures stay openable by the real bdd binary.
package fixture

// schemaDDL creates every table this fixture writes to, plus the indexes
// needed for the query shapes the benchmark harness exercises (get-by-id,
// list-by-status, label filter, edge traversal, note lookup).
const schemaDDL = `
CREATE TABLE schema_versions (
	version    INTEGER NOT NULL,
	applied_at TEXT    NOT NULL
);

CREATE TABLE workspace (
	id_prefix  TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE status_definitions (
	status   TEXT PRIMARY KEY,
	category TEXT NOT NULL
);

CREATE TABLE type_definitions (
	type TEXT PRIMARY KEY
);

CREATE TABLE cards (
	id           TEXT PRIMARY KEY,
	title        TEXT NOT NULL,
	type         TEXT NOT NULL REFERENCES type_definitions(type),
	status       TEXT NOT NULL REFERENCES status_definitions(status),
	priority     INTEGER NOT NULL DEFAULT 2,
	description  TEXT NOT NULL DEFAULT '',
	reproduction TEXT NOT NULL DEFAULT '',
	design       TEXT NOT NULL DEFAULT '',
	acceptance   TEXT NOT NULL DEFAULT '',
	external_ref TEXT NOT NULL DEFAULT '',
	worktree     TEXT NOT NULL DEFAULT '',
	assignee     TEXT NOT NULL DEFAULT '',
	dispatchable INTEGER NOT NULL DEFAULT 1,
	created_by   TEXT NOT NULL,
	created_at   TEXT NOT NULL,
	updated_at   TEXT NOT NULL,
	started_at   TEXT,
	closed_at    TEXT,
	defer_until  TEXT,
	revision     INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX idx_cards_status ON cards(status);
CREATE INDEX idx_cards_type ON cards(type);
CREATE INDEX idx_cards_priority ON cards(priority);
CREATE INDEX idx_cards_updated_at ON cards(updated_at);

CREATE TABLE labels (
	card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
	label   TEXT NOT NULL,
	PRIMARY KEY (card_id, label)
);

CREATE INDEX idx_labels_label ON labels(label);

CREATE TABLE card_edges (
	parent_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
	child_id  TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
	PRIMARY KEY (parent_id, child_id)
);

CREATE INDEX idx_card_edges_child ON card_edges(child_id);
CREATE INDEX idx_card_edges_parent ON card_edges(parent_id);

CREATE TABLE notes (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	card_id    TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
	body       TEXT NOT NULL,
	author     TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE INDEX idx_notes_card_id ON notes(card_id);

CREATE TABLE memories (
	key        TEXT PRIMARY KEY,
	body       TEXT NOT NULL,
	created_by TEXT NOT NULL,
	updated_by TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	revision   INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE runes (
	key        TEXT PRIMARY KEY,
	kind       TEXT NOT NULL,
	title      TEXT NOT NULL,
	body       TEXT NOT NULL,
	metadata   TEXT NOT NULL DEFAULT '',
	enabled    INTEGER NOT NULL DEFAULT 1,
	protected  INTEGER NOT NULL DEFAULT 0,
	created_by TEXT NOT NULL,
	updated_by TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	revision   INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	card_id    TEXT REFERENCES cards(id) ON DELETE CASCADE,
	kind       TEXT NOT NULL,
	actor      TEXT NOT NULL,
	detail     TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);

CREATE INDEX idx_events_card_id ON events(card_id);

CREATE TABLE config (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// builtinStatuses seeds status_definitions with the six built-in statuses
// and their categories, matching bdd.BuiltinStatusCategories.
var builtinStatuses = []struct{ status, category string }{
	{"open", "active"},
	{"in_progress", "wip"},
	{"blocked", "frozen"},
	{"deferred", "frozen"},
	{"closed", "done"},
	{"wontfix", "done"},
}

// builtinTypes seeds type_definitions with the six built-in card types.
var builtinTypes = []string{"bug", "task", "feature", "epic", "decision", "chore"}
