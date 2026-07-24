-- Schema version 4: FTS5 trigram indexes backing SearchCards' substring
-- match. SearchCards' OR'd LIKE '%...%' across 8 columns plus a per-row
-- correlated notes EXISTS forces a full table scan of cards (SCAN cards)
-- at 10k rows, which is the dominant cost in its p50/p95 latency.
--
-- The trigram tokenizer (case_sensitive=0, matching the existing COLLATE
-- NOCASE semantics) lets SQLite's LIKE optimizer satisfy "%pattern%"
-- constraints against these virtual tables with an index lookup per column
-- instead of a full scan (SQLite's MULTI-INDEX OR for the OR'd columns),
-- without changing SearchCards' substring-match semantics or its query
-- surface. content=/content_rowid= makes both tables external-content, so
-- cards/notes stay the single source of truth for the actual text.
CREATE VIRTUAL TABLE cards_fts USING fts5(
  id, title, description, reproduction, design, acceptance, external_ref, worktree,
  content=cards, content_rowid=rowid, tokenize='trigram case_sensitive 0'
);

CREATE VIRTUAL TABLE notes_fts USING fts5(
  card_id UNINDEXED, body,
  content=notes, content_rowid=id, tokenize='trigram case_sensitive 0'
);

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

CREATE TRIGGER notes_fts_ai AFTER INSERT ON notes BEGIN
  INSERT INTO notes_fts(rowid, card_id, body) VALUES (new.id, new.card_id, new.body);
END;

CREATE TRIGGER notes_fts_ad AFTER DELETE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, card_id, body) VALUES ('delete', old.id, old.card_id, old.body);
END;

CREATE TRIGGER notes_fts_au AFTER UPDATE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, card_id, body) VALUES ('delete', old.id, old.card_id, old.body);
  INSERT INTO notes_fts(rowid, card_id, body) VALUES (new.id, new.card_id, new.body);
END;

-- cards.notes cascades ON DELETE (foreign_keys=ON); that cascade delete
-- fires notes_fts_ad per removed note, same as a direct note delete would,
-- so a deleted card's notes are already cleaned out of notes_fts without
-- any extra trigger on cards. (An earlier version of this migration added
-- a redundant BEFORE DELETE ON cards cleanup step, on the mistaken
-- assumption that recursive_triggers gates FK-cascade-fired triggers; it
-- does not, and the two triggers issuing the fts5 'delete' command against
-- the same rows raced, corrupting cards_fts/notes_fts's shadow tables.)

-- Backfill: index every card and note that existed before this migration.
INSERT INTO cards_fts(cards_fts) VALUES ('rebuild');
INSERT INTO notes_fts(notes_fts) VALUES ('rebuild');

-- The NOCASE prefix indexes from migration 0002 backed SearchCards' old
-- indexed-prefix fast path, now superseded by cards_fts above (which covers
-- prefix matches too, as a special case of substring matching). Drop them:
-- they cost every write a maintenance write with no remaining read benefit.
DROP INDEX idx_cards_id_nocase;
DROP INDEX idx_cards_title_nocase;
