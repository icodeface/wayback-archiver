-- Drop created_at column from pages table
ALTER TABLE pages DROP COLUMN IF EXISTS created_at;
