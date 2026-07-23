-- Schema version 6: remove the dispatchable column. It duplicated readiness
-- signal already expressible via status, parent edges, assignment, and the
-- human label, and normal create/update callers had no way to set or clear
-- it. SQLite's DROP COLUMN support varies across versions, so this rebuilds
-- the cards table (rebuild-table-and-copy) rather than relying on it.

CREATE TABLE cards_new (
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
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  started_at   TEXT,
  closed_at    TEXT,
  defer_until  TEXT,
  revision     INTEGER NOT NULL DEFAULT 1,
  owner        TEXT NOT NULL DEFAULT ''
);

INSERT INTO cards_new (
  id, title, worktree, description, reproduction, design, acceptance,
  status, priority, card_type, external_ref, assignee, created_by,
  created_at, updated_at, started_at, closed_at, defer_until, revision, owner
)
SELECT
  id, title, worktree, description, reproduction, design, acceptance,
  status, priority, card_type, external_ref, assignee, created_by,
  created_at, updated_at, started_at, closed_at, defer_until, revision, owner
FROM cards;

-- These triggers belong to the old cards table and would otherwise be
-- dropped along with it; drop them explicitly so the DROP TABLE below is
-- unambiguous, then recreate them on the new table.
DROP TRIGGER cards_fts_ai;
DROP TRIGGER cards_fts_ad;
DROP TRIGGER cards_fts_au;

DROP TABLE cards;
ALTER TABLE cards_new RENAME TO cards;

CREATE INDEX idx_cards_status_category_priority ON cards(status, priority, created_at);
CREATE INDEX idx_cards_priority_created ON cards(priority, created_at);
CREATE INDEX idx_cards_assignee ON cards(assignee);
CREATE INDEX idx_cards_updated_at ON cards(updated_at);
CREATE INDEX idx_cards_worktree ON cards(worktree);

CREATE TRIGGER cards_fts_ai AFTER INSERT ON cards BEGIN
  INSERT INTO cards_fts(rowid, id, title, description, reproduction, design, acceptance, external_ref, worktree)
  VALUES (new.rowid, new.id, new.title, new.description, new.reproduction, new.design, new.acceptance, new.external_ref, new.worktree);
END;

CREATE TRIGGER cards_fts_ad AFTER DELETE ON cards BEGIN
  INSERT INTO cards_fts(cards_fts, rowid, id, title, description, reproduction, design, acceptance, external_ref, worktree)
  VALUES ('delete', old.rowid, old.id, old.title, old.description, old.reproduction, old.design, old.acceptance, old.external_ref, old.worktree);
END;

CREATE TRIGGER cards_fts_au AFTER UPDATE ON cards BEGIN
  INSERT INTO cards_fts(cards_fts, rowid, id, title, description, reproduction, design, acceptance, external_ref, worktree)
  VALUES ('delete', old.rowid, old.id, old.title, old.description, old.reproduction, old.design, old.acceptance, old.external_ref, old.worktree);
  INSERT INTO cards_fts(rowid, id, title, description, reproduction, design, acceptance, external_ref, worktree)
  VALUES (new.rowid, new.id, new.title, new.description, new.reproduction, new.design, new.acceptance, new.external_ref, new.worktree);
END;

-- The rebuilt cards table has fresh rowids, so the fts5 external-content
-- index (keyed by content_rowid=rowid) must be rebuilt from scratch.
INSERT INTO cards_fts(cards_fts) VALUES ('rebuild');
