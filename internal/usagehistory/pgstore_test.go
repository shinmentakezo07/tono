package usagehistory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestHandleUsagePreservesProviderReportedTotalTokens(t *testing.T) {
	tmpDir := t.TempDir()
	oldEnabled := Enabled()
	oldStore := store
	oldWriter := pgWriter
	SetEnabled(true)
	InitStore(tmpDir)
	SetPgWriter(nil)
	t.Cleanup(func() {
		SetEnabled(oldEnabled)
		CloseStore()
		store = oldStore
		pgWriter = oldWriter
	})

	requestedAt := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	plugin := &historyPlugin{}
	plugin.HandleUsage(context.Background(), coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.4",
		Alias:       "client-model",
		AuthType:    "apikey",
		RequestedAt: requestedAt,
		Detail: coreusage.Detail{
			InputTokens:     100,
			OutputTokens:    50,
			ReasoningTokens: 20,
			TotalTokens:     150,
		},
	})
	CloseStore()

	data, err := os.ReadFile(filepath.Join(tmpDir, "usage-2026-06-02.jsonl"))
	if err != nil {
		t.Fatalf("read usage history: %v", err)
	}
	var record JSONLRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode usage history: %v", err)
	}
	if record.EventID == "" {
		t.Fatal("event id is empty")
	}
	if record.Tokens.TotalTokens != 150 {
		t.Fatalf("total tokens = %d, want provider-reported %d", record.Tokens.TotalTokens, 150)
	}
}

func TestPgRecordConversionRoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	orig := JSONLRecord{
		EventID:         "evt-123",
		Provider:        "openai",
		Model:           "gpt-4",
		Alias:           "gpt-4-alias",
		Endpoint:        "/v1/chat/completions",
		AuthType:        "api-key",
		APIKey:          "sk-test123",
		RequestID:       "req-abc-123",
		ReasoningEffort: "medium",
		Timestamp:       ts,
		LatencyMs:       1234,
		Source:          "cli-proxy",
		AuthIndex:       "auth-0",
		Tokens: TokenStats{
			InputTokens:         100,
			OutputTokens:        50,
			ReasoningTokens:     30,
			CachedTokens:        10,
			CacheReadTokens:     5,
			CacheCreationTokens: 3,
			TotalTokens:         180,
		},
		Failed: true,
		Fail: FailDetail{
			StatusCode: 429,
			Body:       "rate limit exceeded",
		},
	}

	pgRec := fromJSONLRecord(&orig)
	result := pgRec.toJSONLRecord()

	if result.EventID != orig.EventID {
		t.Errorf("EventID: got %q, want %q", result.EventID, orig.EventID)
	}
	if result.Provider != orig.Provider {
		t.Errorf("Provider: got %q, want %q", result.Provider, orig.Provider)
	}
	if result.Model != orig.Model {
		t.Errorf("Model: got %q, want %q", result.Model, orig.Model)
	}
	if result.AuthType != orig.AuthType {
		t.Errorf("AuthType: got %q, want %q", result.AuthType, orig.AuthType)
	}
	if !result.Timestamp.Equal(orig.Timestamp) {
		t.Errorf("Timestamp: got %v, want %v", result.Timestamp, orig.Timestamp)
	}
	if result.LatencyMs != orig.LatencyMs {
		t.Errorf("LatencyMs: got %d, want %d", result.LatencyMs, orig.LatencyMs)
	}
	if result.Tokens.TotalTokens != orig.Tokens.TotalTokens {
		t.Errorf("TotalTokens: got %d, want %d", result.Tokens.TotalTokens, orig.Tokens.TotalTokens)
	}
	if result.Failed != orig.Failed {
		t.Errorf("Failed: got %v, want %v", result.Failed, orig.Failed)
	}
	if result.Fail.StatusCode != orig.Fail.StatusCode {
		t.Errorf("FailStatusCode: got %d, want %d", result.Fail.StatusCode, orig.Fail.StatusCode)
	}
	if result.Fail.Body != orig.Fail.Body {
		t.Errorf("FailBody: got %q, want %q", result.Fail.Body, orig.Fail.Body)
	}
}

func TestPgRecordConversionDefaults(t *testing.T) {
	// Ensure that zero-value JSONLRecord produces a valid PgRecord.
	orig := JSONLRecord{}
	pgRec := fromJSONLRecord(&orig)
	result := pgRec.toJSONLRecord()

	if result.Provider != "" {
		t.Errorf("expected empty Provider, got %q", result.Provider)
	}
	if result.Tokens.TotalTokens != 0 {
		t.Errorf("expected 0 TotalTokens, got %d", result.Tokens.TotalTokens)
	}
	if result.Failed {
		t.Errorf("expected Failed=false, got true")
	}
}

func TestWriterWriteDrain(t *testing.T) {
	// Test that the writer can accept records and drain them on stop.
	// The Writer contract is now "no drops": Write always succeeds, the
	// internal queue is drained into the batch.
	store := &mockStore{}
	w := NewWriter(nil, 10, 100, 10*time.Millisecond)
	w.SetInserter(store)
	w.Start(context.Background())

	rec := fromJSONLRecord(&JSONLRecord{
		Model:  "test-model",
		Tokens: TokenStats{TotalTokens: 100},
	})

	if !w.Write(rec) {
		t.Fatal("Write should return true")
	}

	// Stop drains remaining records and flushes.
	w.Stop()

	if got := store.TotalRecords(); got != 1 {
		t.Errorf("expected 1 drained record, got %d", got)
	}
}

// TestWriterWriteNeverDrops is the regression test for the production bug
// where the bounded channel silently dropped records past 1000. The current
// contract is that Write never returns false on a non-closed Writer,
// regardless of how many records have been enqueued.
func TestWriterWriteNeverDrops(t *testing.T) {
	store := &mockStore{}
	w := NewWriter(nil, 1, 1, 10*time.Millisecond)
	w.SetInserter(store)
	w.Start(context.Background())

	// Way more records than the (no longer used) buffer size.
	const N = 5000
	for i := 0; i < N; i++ {
		if !w.Write(PgRecord{Model: "burst"}) {
			t.Fatalf("Write returned false at record %d — drops are not allowed", i)
		}
	}

	// Stop drains remaining records and flushes.
	w.Stop()

	if got := store.TotalRecords(); got != N {
		t.Fatalf("expected all %d records stored, got %d (lost %d)", N, got, N-got)
	}
}
