-- Schema version 5: preserve the source system's owner as immutable card
-- metadata. Owner is distinct from assignee (the current worker) and
-- created_by (the record creator).
ALTER TABLE cards ADD COLUMN owner TEXT NOT NULL DEFAULT '';
