-- Schema version 6: let a rune declare how `bdd prime` should surface it.
-- 'required' runes are inlined in full in the prime bootstrap, 'optional'
-- runes appear only as a key/title/kind/revision summary, and 'never' runes
-- are omitted entirely.
ALTER TABLE runes ADD COLUMN prime TEXT NOT NULL DEFAULT 'optional';
