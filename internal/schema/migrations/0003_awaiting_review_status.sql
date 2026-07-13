-- Schema version 3: bring workspaces created before the 0001 seed fix (bd
-- bdd-zj9) up to the plan's built-in status set. Those workspaces already
-- ran the old 0001_initial.sql and so still have the built-in wontfix
-- status and lack awaiting_review; this migration adds the one and retires
-- the other without breaking any card that already uses wontfix.

INSERT INTO status_definitions (name, category, built_in)
VALUES ('awaiting_review', 'wip', 1)
ON CONFLICT(name) DO NOTHING;

-- Only the legacy built-in wontfix definition is retired here; a
-- workspace may separately define its own custom wontfix status, which
-- must be left untouched.
--
-- If a card still references the built-in wontfix, keep the definition
-- (as a plain custom status, so FK integrity holds) instead of deleting
-- it out from under that card.
UPDATE status_definitions
SET built_in = 0
WHERE name = 'wontfix'
  AND built_in = 1
  AND EXISTS (SELECT 1 FROM cards WHERE status = 'wontfix');

DELETE FROM status_definitions
WHERE name = 'wontfix'
  AND built_in = 1
  AND NOT EXISTS (SELECT 1 FROM cards WHERE status = 'wontfix');
