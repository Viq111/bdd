-- Schema version 1: initial logical schema (bdd plan section 18) plus
-- seed data for the six built-in statuses and six built-in types.

CREATE TABLE schema_versions (
  version    INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE workspace (
  singleton  INTEGER PRIMARY KEY CHECK (singleton = 1),
  prefix     TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE status_definitions (
  name     TEXT PRIMARY KEY,
  category TEXT NOT NULL,
  built_in INTEGER NOT NULL
);

CREATE TABLE type_definitions (
  name     TEXT PRIMARY KEY,
  built_in INTEGER NOT NULL
);

CREATE TABLE cards (
  id           TEXT PRIMARY KEY,
  title        TEXT NOT NULL,
  worktree     TEXT NOT NULL DEFAULT '',
  description  TEXT NOT NULL DEFAULT '',
  reproduction TEXT NOT NULL DEFAULT '',
  design       TEXT NOT NULL DEFAULT '',
  acceptance   TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL REFERENCES status_definitions(name),
  priority     INTEGER NOT NULL DEFAULT 2 CHECK (priority BETWEEN 0 AND 2147483647),
  card_type    TEXT NOT NULL REFERENCES type_definitions(name),
  external_ref TEXT NOT NULL DEFAULT '',
  assignee     TEXT NOT NULL DEFAULT '',
  created_by   TEXT NOT NULL DEFAULT '',
  dispatchable INTEGER NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  started_at   TEXT,
  closed_at    TEXT,
  defer_until  TEXT,
  revision     INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE labels (
  card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  label   TEXT NOT NULL,
  PRIMARY KEY (card_id, label)
);

CREATE TABLE card_edges (
  parent_id  TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  child_id   TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  created_by TEXT,
  PRIMARY KEY (parent_id, child_id),
  CHECK (parent_id <> child_id)
);

CREATE TABLE notes (
  id         INTEGER PRIMARY KEY,
  card_id    TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  author     TEXT,
  body       TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE memories (
  key        TEXT PRIMARY KEY,
  body       TEXT NOT NULL,
  created_by TEXT,
  updated_by TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  revision   INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE runes (
  key           TEXT PRIMARY KEY,
  kind          TEXT NOT NULL,
  title         TEXT NOT NULL,
  body          TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  enabled       INTEGER NOT NULL DEFAULT 1,
  protected     INTEGER NOT NULL DEFAULT 0,
  created_by    TEXT,
  updated_by    TEXT,
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL,
  revision      INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE events (
  id           INTEGER PRIMARY KEY,
  subject_kind TEXT NOT NULL,
  subject_key  TEXT NOT NULL,
  revision     INTEGER NOT NULL,
  action       TEXT NOT NULL,
  actor        TEXT,
  payload_json TEXT NOT NULL,
  created_at   TEXT NOT NULL
);

CREATE TABLE config (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  updated_by TEXT
);

CREATE INDEX idx_cards_status_category_priority ON cards(status, priority, created_at);
CREATE INDEX idx_cards_priority_created ON cards(priority, created_at);
CREATE INDEX idx_cards_assignee ON cards(assignee);
CREATE INDEX idx_cards_updated_at ON cards(updated_at);
CREATE INDEX idx_cards_worktree ON cards(worktree);
CREATE INDEX idx_labels_label ON labels(label);
CREATE INDEX idx_card_edges_child ON card_edges(child_id);
CREATE INDEX idx_card_edges_parent ON card_edges(parent_id);
CREATE INDEX idx_notes_card_created ON notes(card_id, created_at);
CREATE INDEX idx_memories_updated_at ON memories(updated_at);
CREATE INDEX idx_runes_kind_enabled_updated ON runes(kind, enabled, updated_at);

-- Built-in statuses (bdd plan section 10 / card.go BuiltinStatusCategories).
INSERT INTO status_definitions (name, category, built_in) VALUES
  ('open',        'active', 1),
  ('in_progress', 'wip',    1),
  ('blocked',     'frozen', 1),
  ('deferred',    'frozen', 1),
  ('closed',      'done',   1),
  ('wontfix',     'done',   1);

-- Built-in types (bdd plan section 10 / card.go CardType constants).
INSERT INTO type_definitions (name, built_in) VALUES
  ('bug', 1),
  ('task', 1),
  ('feature', 1),
  ('epic', 1),
  ('decision', 1),
  ('chore', 1);
