ALTER TABLE pages ADD COLUMN IF NOT EXISTS snapshot_state VARCHAR(16);

UPDATE pages
SET snapshot_state = 'ready'
WHERE snapshot_state IS NULL OR snapshot_state = '';

ALTER TABLE pages ALTER COLUMN snapshot_state SET DEFAULT 'pending';
ALTER TABLE pages ALTER COLUMN snapshot_state SET NOT NULL;
