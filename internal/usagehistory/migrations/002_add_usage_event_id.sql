-- +goose Up
-- Add idempotency key for retry-safe usage history writes.

ALTER TABLE usage_records
    ADD COLUMN IF NOT EXISTS event_id TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_records_event_id_created_at
    ON usage_records (event_id, created_at)
    WHERE event_id <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_usage_records_event_id_created_at;
ALTER TABLE usage_records
    DROP COLUMN IF EXISTS event_id;
