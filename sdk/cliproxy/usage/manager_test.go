package usage

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// noopPlugin counts invocations but discards the record. Used to isolate the
// Manager's queue mechanics from any real plugin side effects.
type noopPlugin struct {
	handled atomic.Int64
}

func (p *noopPlugin) HandleUsage(ctx context.Context, record Record) {
	p.handled.Add(1)
}

// TestManagerQueueCompactsAfterDrain is the regression test for the slice
// memory leak introduced when the channel-based queue was replaced with
// `m.queue = m.queue[1:]`. After draining N records, the slice header should
// advance and the underlying array should be released (or set to nil) so the
// GC can reclaim the records.
func TestManagerQueueCompactsAfterDrain(t *testing.T) {
	m := NewManager(0)
	plugin := &noopPlugin{}
	m.Register(plugin)
	m.Start(context.Background())
	defer m.Stop()

	const N = 10000
	for i := 0; i < N; i++ {
		m.Publish(context.Background(), Record{Provider: "p", Model: "m"})
	}

	// Wait for the dispatcher to drain everything.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if plugin.handled.Load() >= N {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := plugin.handled.Load(); got != N {
		t.Fatalf("expected %d handled, got %d", N, got)
	}

	// Give the dispatcher a tick to compact the slice.
	time.Sleep(50 * time.Millisecond)

	// Inspect the queue. The slice header should have advanced past the
	// processed records, and the underlying array should have been released.
	// With the old `m.queue = m.queue[1:]` code, cap stayed at N; with the
	// fix, cap is reset to a small value (or nil).
	m.mu.Lock()
	queueLen := len(m.queue)
	queueCap := cap(m.queue)
	m.mu.Unlock()

	if queueLen != 0 {
		t.Errorf("queue should be empty after drain, got len=%d", queueLen)
	}
	// 64 is the compaction threshold used in the fix. Anything > N/100
	// indicates the underlying array is still holding onto the old records.
	if queueCap > 100 {
		t.Errorf("queue underlying array leaked: cap=%d after processing %d records", queueCap, N)
	}
}

// TestManagerHandlesBurstWithoutUnboundedGrowth makes sure that under a
// sustained burst, the Manager's memory footprint does not grow without
// bound. We measure total Alloc growth across 5 rounds of N=2000 publishes,
// each round followed by a forced GC and snapshot.
func TestManagerHandlesBurstWithoutUnboundedGrowth(t *testing.T) {
	m := NewManager(0)
	plugin := &noopPlugin{}
	m.Register(plugin)
	m.Start(context.Background())
	defer m.Stop()

	measure := func() uint64 {
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.HeapAlloc
	}

	const rounds = 5
	const perRound = 2000
	baseline := measure()

	var maxDelta uint64
	for r := 0; r < rounds; r++ {
		for i := 0; i < perRound; i++ {
			m.Publish(context.Background(), Record{Provider: "p", Model: "m"})
		}
		// Wait for the dispatcher to catch up before next round.
		expected := int64((r + 1) * perRound)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if plugin.handled.Load() >= expected {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
		now := measure()
		if now > baseline && now-baseline > maxDelta {
			maxDelta = now - baseline
		}
	}

	// We allow some growth for the plugin dispatch closures, but the slice
	// leak (old `m.queue = m.queue[1:]`) would push this into the tens of
	// MB. After the fix, growth is bounded by the dispatcher queue's
	// working set.
	const maxAllowed = 4 * 1024 * 1024 // 4 MiB
	if maxDelta > maxAllowed {
		t.Errorf("memory grew by %d bytes across rounds; leak suspected (max allowed %d)", maxDelta, maxAllowed)
	}
}
