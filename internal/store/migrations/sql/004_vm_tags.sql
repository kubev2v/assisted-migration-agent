-- Pre-computed group-to-VM matches from filter evaluation
CREATE TABLE IF NOT EXISTS group_matches (
    group_id INTEGER PRIMARY KEY,
    vm_ids VARCHAR[]
);
