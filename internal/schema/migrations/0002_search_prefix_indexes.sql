-- Case-insensitive indexes backing SearchCards' indexed prefix checks (plan
-- section 18): id and title are the two fields users most often search by a
-- typed prefix, so a NOCASE index lets SQLite's LIKE optimizer satisfy
-- "prefix%" patterns with an index range scan instead of a full table scan.
CREATE INDEX idx_cards_id_nocase ON cards(id COLLATE NOCASE);
CREATE INDEX idx_cards_title_nocase ON cards(title COLLATE NOCASE);
