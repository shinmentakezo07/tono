package usagehistory

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

var (
	enabled  atomic.Bool
	store    *Store  // existing JSONL store
	pgWriter *Writer // async TimescaleDB writer (nil when Postgres not configured)
)

func init() {
	coreusage.RegisterPlugin(&historyPlugin{})
}

// SetEnabled toggles the usage history plugin at runtime.
func SetEnabled(v bool) {
	enabled.Store(v)
}

// Enabled returns whether the usage history plugin is active.
func Enabled() bool {
	return enabled.Load()
}

// InitStore initializes the global store with the given directory.
func InitStore(dir string) {
	if store != nil {
		_ = store.Close()
	}
	store = NewStore(dir)
}

// CloseStore closes the global store. Call on shutdown.
func CloseStore() {
	if store != nil {
		_ = store.Close()
	}
}

// SetPgWriter sets the async TimescaleDB writer. Must be called before any usage records are processed.
func SetPgWriter(w *Writer) {
	pgWriter = w
}

// StopPgWriter stops the async TimescaleDB writer, flushing remaining records.
func StopPgWriter() {
	if pgWriter != nil {
		pgWriter.Stop()
	}
}

// HasPgStore returns true if the TimescaleDB backend is available.
func HasPgStore() bool {
	return pgWriter != nil && pgWriter.store != nil
}

// QueryHistory queries the TimescaleDB store for historical records.
// Returns an error if PgStore is not initialized.
func QueryHistory(ctx context.Context, since time.Time, limit int) ([]JSONLRecord, error) {
	if pgWriter == nil || pgWriter.store == nil {
		return nil, fmt.Errorf("usagehistory: TimescaleDB store not initialized")
	}
	return pgWriter.store.QueryHistory(ctx, since, limit)
}

type historyPlugin struct{}

func (p *historyPlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if p == nil || !Enabled() || store == nil {
		return
	}

	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	model := strings.TrimSpace(record.Model)
	if model == "" {
		model = "unknown"
	}
	alias := strings.TrimSpace(record.Alias)
	if alias == "" {
		alias = model
	}
	provider := strings.TrimSpace(record.Provider)
	if provider == "" {
		provider = "unknown"
	}
	authType := strings.TrimSpace(record.AuthType)
	if authType == "" {
		authType = "unknown"
	}

	totalTokens := record.Detail.InputTokens + record.Detail.OutputTokens + record.Detail.ReasoningTokens
	if totalTokens == 0 {
		totalTokens = record.Detail.InputTokens + record.Detail.OutputTokens + record.Detail.ReasoningTokens + record.Detail.CachedTokens
	}

	rec := JSONLRecord{
		Provider:        provider,
		Model:           model,
		Alias:           alias,
		Endpoint:        strings.TrimSpace(logging.GetEndpoint(ctx)),
		AuthType:        authType,
		APIKey:          strings.TrimSpace(record.APIKey),
		RequestID:       strings.TrimSpace(logging.GetRequestID(ctx)),
		ReasoningEffort: strings.TrimSpace(record.ReasoningEffort),
		Timestamp:       timestamp,
		LatencyMs:       record.Latency.Milliseconds(),
		Source:          record.Source,
		AuthIndex:       record.AuthIndex,
		Tokens: TokenStats{
			InputTokens:         record.Detail.InputTokens,
			OutputTokens:        record.Detail.OutputTokens,
			ReasoningTokens:     record.Detail.ReasoningTokens,
			CachedTokens:        record.Detail.CachedTokens,
			CacheReadTokens:     record.Detail.CacheReadTokens,
			CacheCreationTokens: record.Detail.CacheCreationTokens,
			TotalTokens:         totalTokens,
		},
		Failed: record.Failed,
		Fail: FailDetail{
			StatusCode: record.Fail.StatusCode,
			Body:       strings.TrimSpace(record.Fail.Body),
		},
	}

	if err := store.Write(rec); err != nil {
		log.WithError(err).Warn("usagehistory: failed to write record")
	}

	// Write to TimescaleDB via async writer (if configured).
	if pgWriter != nil {
		pgWriter.Write(fromJSONLRecord(&rec))
	}
}
