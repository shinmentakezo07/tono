-- +goose Up
-- Usage records hypertable for time-series storage.
-- Requires TimescaleDB extension: CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE IF NOT EXISTS usage_records (
    event_id            TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    provider            TEXT NOT NULL DEFAULT 'unknown',
    model               TEXT NOT NULL DEFAULT 'unknown',
    alias               TEXT NOT NULL DEFAULT '',
    endpoint            TEXT NOT NULL DEFAULT '',
    auth_type           TEXT NOT NULL DEFAULT 'unknown',
    api_key             TEXT NOT NULL DEFAULT '',
    request_id          TEXT NOT NULL DEFAULT '',
    reasoning_effort    TEXT NOT NULL DEFAULT '',
    latency_ms          BIGINT NOT NULL DEFAULT 0,
    source              TEXT NOT NULL DEFAULT '',
    auth_index          TEXT NOT NULL DEFAULT '',
    input_tokens        BIGINT NOT NULL DEFAULT 0,
    output_tokens       BIGINT NOT NULL DEFAULT 0,
    reasoning_tokens    BIGINT NOT NULL DEFAULT 0,
    cached_tokens       BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens   BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    total_tokens        BIGINT NOT NULL DEFAULT 0,
    failed              BOOLEAN NOT NULL DEFAULT FALSE,
    fail_status_code    INT NOT NULL DEFAULT 0,
    fail_body           TEXT NOT NULL DEFAULT ''
);

-- Convert to TimescaleDB hypertable (chunk interval: 1 day).
-- Wrapped in a DO block so the migration succeeds even if TimescaleDB is not installed.
DO $$
BEGIN
    PERFORM create_hypertable('usage_records', 'created_at',
        chunk_time_interval => INTERVAL '1 day',
        if_not_exists => TRUE
    );
EXCEPTION
    WHEN OTHERS THEN
        RAISE WARNING 'create_hypertable failed (TimescaleDB not installed?) — table works as regular Postgres: %', SQLERRM;
END $$;

-- Idempotency for retry-safe writes. Include created_at because TimescaleDB
-- requires unique indexes on hypertables to cover the partitioning column.
CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_records_event_id_created_at
    ON usage_records (event_id, created_at)
    WHERE event_id <> '';

-- Compound index: "usage per model per day", "top accounts"
CREATE INDEX IF NOT EXISTS idx_usage_records_provider_model_key
    ON usage_records (provider, model, api_key, created_at DESC);

-- Index for time-range queries
CREATE INDEX IF NOT EXISTS idx_usage_records_created_at
    ON usage_records (created_at DESC);

-- Index for per-api-key queries
CREATE INDEX IF NOT EXISTS idx_usage_records_api_key
    ON usage_records (api_key, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS usage_records;
