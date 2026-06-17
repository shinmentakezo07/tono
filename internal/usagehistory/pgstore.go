package usagehistory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // register pgx driver for database/sql (goose)
	"github.com/pressly/goose/v3"
	log "github.com/sirupsen/logrus"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func init() {
	goose.SetBaseFS(migrationsFS)
}

// PgStore manages writing usage records to TimescaleDB via pgx/v5.
// It coexists with the JSONL Store — both can be active simultaneously.
type PgStore struct {
	pool *pgxpool.Pool
}

// NewPgStore creates a PgStore connected to the given Postgres DSN.
func NewPgStore(ctx context.Context, dsn string) (*PgStore, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("usagehistory: parse DSN: %w", err)
	}
	poolCfg.MaxConns = 4
	poolCfg.MinConns = 1
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("usagehistory: create pool: %w", err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("usagehistory: ping: %w", err)
	}
	return &PgStore{pool: pool}, nil
}

// EnsureSchema runs goose migrations to create or update the usage_records table.
// Uses an ephemeral database/sql connection for migrations; the pgxpool is used
// for all runtime queries. The create_hypertable call is wrapped in a DO $$ EXCEPTION
// block inside the migration so it succeeds even without TimescaleDB installed.
func (s *PgStore) EnsureSchema(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("usagehistory: open sql db for migrations: %w", err)
	}
	defer db.Close()

	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("usagehistory: goose up: %w", err)
	}
	return nil
}

// SetRetentionPolicy configures TimescaleDB auto-drop for old chunks.
func (s *PgStore) SetRetentionPolicy(ctx context.Context, days int) error {
	if days <= 0 {
		return nil
	}
	_, _ = s.pool.Exec(ctx, `SELECT remove_retention_policy('usage_records', if_exists => TRUE)`)
	query := fmt.Sprintf(`SELECT add_retention_policy('usage_records', INTERVAL '%d days')`, days)
	if _, err := s.pool.Exec(ctx, query); err != nil {
		return fmt.Errorf("usagehistory: set retention policy: %w", err)
	}
	return nil
}

// InsertBatch inserts multiple records in a single pgx batch for throughput.
func (s *PgStore) InsertBatch(ctx context.Context, records []PgRecord) error {
	if len(records) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	query := `
		INSERT INTO usage_records (
			event_id, created_at, provider, model, alias, endpoint, auth_type, api_key,
			request_id, reasoning_effort, latency_ms, source, auth_index,
			input_tokens, output_tokens, reasoning_tokens, cached_tokens,
			cache_read_tokens, cache_creation_tokens, total_tokens,
			failed, fail_status_code, fail_body
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13,
			$14, $15, $16, $17,
			$18, $19, $20,
			$21, $22, $23
		)
		ON CONFLICT (event_id, created_at) WHERE event_id <> '' DO NOTHING`

	for i := range records {
		r := &records[i]
		batch.Queue(query,
			pgRecordEventID(r), r.CreatedAt, r.Provider, r.Model, r.Alias, r.Endpoint, r.AuthType, r.APIKey,
			r.RequestID, r.ReasoningEffort, r.LatencyMs, r.Source, r.AuthIndex,
			r.InputTokens, r.OutputTokens, r.ReasoningTokens, r.CachedTokens,
			r.CacheReadTokens, r.CacheCreationTokens, r.TotalTokens,
			r.Failed, r.FailStatusCode, r.FailBody,
		)
	}

	br := s.pool.SendBatch(ctx, batch)
	defer func() {
		if err := br.Close(); err != nil {
			log.WithError(err).Warn("usagehistory: batch close error")
		}
	}()

	for i := 0; i < len(records); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("usagehistory: batch exec record %d: %w", i, err)
		}
	}
	return nil
}

// QueryHistory retrieves usage records within a time range.
// Returns records in JSONLRecord-compatible format for the management handler.
// If limit <= 0, no LIMIT clause is applied and every row in the window is returned.
func (s *PgStore) QueryHistory(ctx context.Context, since time.Time, limit int) ([]JSONLRecord, error) {
	query := `
		SELECT event_id, created_at, provider, model, alias, endpoint, auth_type, api_key,
			request_id, reasoning_effort, latency_ms, source, auth_index,
			input_tokens, output_tokens, reasoning_tokens, cached_tokens,
			cache_read_tokens, cache_creation_tokens, total_tokens,
			failed, fail_status_code, fail_body
		FROM usage_records
		WHERE created_at >= $1
		ORDER BY created_at DESC`
	args := []any{since}
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("usagehistory: query: %w", err)
	}
	defer rows.Close()

	var records []JSONLRecord
	for rows.Next() {
		var r PgRecord
		if err := rows.Scan(
			&r.EventID, &r.CreatedAt, &r.Provider, &r.Model, &r.Alias, &r.Endpoint, &r.AuthType, &r.APIKey,
			&r.RequestID, &r.ReasoningEffort, &r.LatencyMs, &r.Source, &r.AuthIndex,
			&r.InputTokens, &r.OutputTokens, &r.ReasoningTokens, &r.CachedTokens,
			&r.CacheReadTokens, &r.CacheCreationTokens, &r.TotalTokens,
			&r.Failed, &r.FailStatusCode, &r.FailBody,
		); err != nil {
			return nil, fmt.Errorf("usagehistory: scan: %w", err)
		}
		records = append(records, r.toJSONLRecord())
	}
	return records, rows.Err()
}

// pgRecordEventID returns the record's idempotency key, generating a non-secret
// deterministic fallback for package-local tests or legacy call sites.
func pgRecordEventID(r *PgRecord) string {
	if r == nil {
		return ""
	}
	if id := strings.TrimSpace(r.EventID); id != "" {
		return id
	}
	parts := []string{
		strings.TrimSpace(r.Provider),
		strings.TrimSpace(r.Model),
		strings.TrimSpace(r.Alias),
		strings.TrimSpace(r.Endpoint),
		strings.TrimSpace(r.AuthType),
		strings.TrimSpace(r.RequestID),
		strings.TrimSpace(r.Source),
		strings.TrimSpace(r.AuthIndex),
		fmt.Sprintf("%d/%d/%d/%d/%d/%d/%d", r.InputTokens, r.OutputTokens, r.ReasoningTokens, r.CachedTokens, r.CacheReadTokens, r.CacheCreationTokens, r.TotalTokens),
		fmt.Sprintf("%t/%d", r.Failed, r.FailStatusCode),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// Close closes the connection pool.
func (s *PgStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}
