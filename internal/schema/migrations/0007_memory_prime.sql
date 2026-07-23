-- Schema version 7: let a memory declare how `bdd prime` should surface
-- it, mirroring the rune prime field added in schema version 6. 'required'
-- memories are inlined in full in the prime bootstrap; 'optional' (the
-- default) memories appear only as a key/first-line summary. Memories have
-- no 'never' designation — --no-memories already covers that case.
ALTER TABLE memories ADD COLUMN prime TEXT NOT NULL DEFAULT 'optional';
