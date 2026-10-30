-- +goose Up
ALTER TABLE users ADD COLUMN last_seen_at timestamptz;
CREATE INDEX users_org_last_seen_idx ON users (org_id, last_seen_at DESC);

-- +goose Down
DROP INDEX users_org_last_seen_idx;
ALTER TABLE users DROP COLUMN last_seen_at;
