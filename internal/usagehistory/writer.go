package usagehistory

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

var (
	metricsRecordsWritten = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cliproxy",
		Subsystem: "usage_history",
		Name:      "records_written_total",
		Help:      "Total usage records written to TimescaleDB.",
	})
	metricsFlushErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cliproxy",
		Subsystem: "usage_history",
		Name:      "flush_errors_total",
		Help:      "Total batch flush errors to TimescaleDB.",
	})
	metricsRecordsDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cliproxy",
		Subsystem: "usage_history",
		Name:      "records_dropped_total",
		Help:      "Total usage records dropped due to full buffer.",
	})
)

func init() {
	prometheus.MustRegister(metricsRecordsWritten, metricsFlushErrors, metricsRecordsDropped)
}

// Writer buffers usage records in a channel and flushes them to PgStore
// in batches. It never blocks the caller on a DB write.
type Writer struct {
	store         *PgStore
	ch            chan PgRecord
	batchSize     int
	flushInterval time.Duration
	done          chan struct{}
	wg            sync.WaitGroup
}

// NewWriter creates a Writer that buffers records and flushes to PgStore.
func NewWriter(store *PgStore, bufferSize, batchSize int, flushInterval time.Duration) *Writer {
	return &Writer{
		store:         store,
		ch:            make(chan PgRecord, bufferSize),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		done:          make(chan struct{}),
	}
}

// Start launches the background flush goroutine.
func (w *Writer) Start(ctx context.Context) {
	w.wg.Add(1)
	go w.run(ctx)
}

// Write enqueues a record for async insertion. Never blocks the caller.
// Returns false if the buffer is full (record dropped).
func (w *Writer) Write(r PgRecord) bool {
	select {
	case w.ch <- r:
		return true
	default:
		metricsRecordsDropped.Inc()
		log.Warn("usagehistory: write buffer full, dropping record")
		return false
	}
}

// Stop signals the writer to flush remaining records and shut down.
func (w *Writer) Stop() {
	close(w.done)
	w.wg.Wait()
}

func (w *Writer) run(ctx context.Context) {
	defer w.wg.Done()

	batch := make([]PgRecord, 0, w.batchSize)
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			w.drain(&batch)
			if len(batch) > 0 {
				w.flush(context.Background(), batch)
			}
			return

		case r := <-w.ch:
			batch = append(batch, r)
			if len(batch) >= w.batchSize {
				w.flush(ctx, batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				w.flush(ctx, batch)
				batch = batch[:0]
			}

		case <-ctx.Done():
			w.drain(&batch)
			if len(batch) > 0 {
				w.flush(context.Background(), batch)
			}
			return
		}
	}
}

func (w *Writer) drain(batch *[]PgRecord) {
	for {
		select {
		case r := <-w.ch:
			*batch = append(*batch, r)
		default:
			return
		}
	}
}

func (w *Writer) flush(ctx context.Context, batch []PgRecord) {
	flushCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := w.store.InsertBatch(flushCtx, batch); err != nil {
		log.WithError(err).WithField("count", len(batch)).Error("usagehistory: batch flush failed")
		metricsFlushErrors.Inc()
	} else {
		metricsRecordsWritten.Add(float64(len(batch)))
	}
}
