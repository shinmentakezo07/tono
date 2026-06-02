package usagehistory

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockStore records every InsertBatch call so we can assert no records
// were silently dropped under burst load.
type mockStore struct {
	mu      sync.Mutex
	batches [][]PgRecord
	delay   time.Duration
}

func (m *mockStore) InsertBatch(ctx context.Context, records []PgRecord) error {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]PgRecord, len(records))
	copy(cp, records)
	m.batches = append(m.batches, cp)
	return nil
}

func (m *mockStore) TotalRecords() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, b := range m.batches {
		total += len(b)
	}
	return total
}

// TestWriterDoesNotDropRecordsUnderBurst proves the Writer does not silently
// drop records when the producer outpaces the consumer. The contract is that
// every record passed to Write is eventually handed to the store.
//
// Currently the Writer uses a bounded channel with non-blocking Write — when
// the buffer fills, additional records are dropped. This test will fail until
// that behaviour is changed to a non-dropping strategy.
func TestWriterDoesNotDropRecordsUnderBurst(t *testing.T) {
	store := &mockStore{}
	// Tiny buffer (5) and slow flush (1s) to expose the drop path.
	w := NewWriter(nil, 5, 100, 100*time.Millisecond)
	w.SetInserter(store)
	w.Start(context.Background())

	const N = 1000
	for i := 0; i < N; i++ {
		w.Write(PgRecord{Model: "test", TotalTokens: int64(i)})
	}

	w.Stop()

	got := store.TotalRecords()
	if got != N {
		t.Fatalf("Writer silently dropped records: wrote %d, stored %d (lost %d)", N, got, N-got)
	}
}

// TestWriterUnlimitedQueueDoesNotBlock is the user-facing requirement: the
// Writer must accept every record without ever returning false. The previous
// API returned bool; this test pins the new contract: Write must not signal
// failure to the caller regardless of burst size.
func TestWriterWriteNeverReportsDrop(t *testing.T) {
	store := &mockStore{}
	w := NewWriter(nil, 1, 1, 10*time.Millisecond)
	w.SetInserter(store)
	w.Start(context.Background())
	defer w.Stop()

	// Fire a burst that massively exceeds the channel buffer.
	var dropped int64
	var wg sync.WaitGroup
	for i := 0; i < 10000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !w.Write(PgRecord{Model: "burst"}) {
				atomic.AddInt64(&dropped, 1)
			}
		}()
	}
	wg.Wait()

	if dropped > 0 {
		t.Fatalf("Writer reported %d drops under burst; contract is zero drops", dropped)
	}
}
