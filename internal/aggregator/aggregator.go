package aggregator

import (
	"context"
	"log"
	"sync"
	"time"

	"tuntrace/internal/collector"
	"tuntrace/internal/store"
)

// Aggregator buckets per-connection deltas into 1-minute windows keyed on the
// full attribution tuple, then flushes completed buckets to SQLite. Buckets
// for the in-progress minute are kept in memory until the wall clock advances
// past their end so a tail of latency-laggy poll results can still merge in.
type Aggregator struct {
	st  *store.Store
	now func() time.Time

	mu      sync.Mutex
	buckets map[bucketKey]*bucketVal
}

type bucketKey struct {
	BucketMinute int64
	ProcessName  string
	ProcessPath  string
	Host         string
	DestIP       string
	Outbound     string
	Chains       string
	Network      string
}

type bucketVal struct {
	Upload    int64
	Download  int64
	ConnCount int64
}

func New(st *store.Store) *Aggregator {
	return &Aggregator{
		st:      st,
		now:     time.Now,
		buckets: make(map[bucketKey]*bucketVal),
	}
}

// SetNow lets tests freeze the clock.
func (a *Aggregator) SetNow(fn func() time.Time) { a.now = fn }

// IngestDeltas adds collector output into the in-memory buckets. observedAt
// determines which minute each delta lands in.
func (a *Aggregator) IngestDeltas(_ context.Context, deltas []collector.Delta, observedAt time.Time) error {
	if len(deltas) == 0 {
		return nil
	}
	minute := observedAt.Unix() / 60

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, d := range deltas {
		k := bucketKey{
			BucketMinute: minute,
			ProcessName:  d.ProcessName,
			ProcessPath:  d.ProcessPath,
			Host:         d.Host,
			DestIP:       d.DestIP,
			Outbound:     d.Outbound,
			Chains:       d.ChainsJSON,
			Network:      d.Network,
		}
		v, ok := a.buckets[k]
		if !ok {
			v = &bucketVal{}
			a.buckets[k] = v
		}
		v.Upload += d.Upload
		v.Download += d.Download
		v.ConnCount++
	}
	return nil
}

// FlushCompleted writes every bucket whose minute is strictly older than the
// current wall-clock minute, then drops them from memory. The in-progress
// minute stays in memory.
func (a *Aggregator) FlushCompleted(ctx context.Context) error {
	cutoff := a.now().Unix() / 60
	a.mu.Lock()
	rows := make([]store.AggregateRow, 0, len(a.buckets))
	completed := make([]bucketKey, 0, len(a.buckets))
	for k, v := range a.buckets {
		if k.BucketMinute < cutoff {
			rows = append(rows, store.AggregateRow{
				BucketMinute: k.BucketMinute,
				ProcessName:  k.ProcessName,
				ProcessPath:  k.ProcessPath,
				Host:         k.Host,
				DestIP:       k.DestIP,
				Outbound:     k.Outbound,
				Chains:       k.Chains,
				Network:      k.Network,
				Upload:       v.Upload,
				Download:     v.Download,
				ConnCount:    v.ConnCount,
			})
			completed = append(completed, k)
		}
	}
	a.mu.Unlock()

	if len(rows) == 0 {
		return nil
	}
	if err := a.st.InsertAggregateBatch(ctx, rows); err != nil {
		return err
	}
	a.mu.Lock()
	for _, k := range completed {
		delete(a.buckets, k)
	}
	a.mu.Unlock()
	return nil
}

// FlushAll forces every bucket out, including the in-progress minute. Used at shutdown.
func (a *Aggregator) FlushAll(ctx context.Context) error {
	a.mu.Lock()
	rows := make([]store.AggregateRow, 0, len(a.buckets))
	for k, v := range a.buckets {
		rows = append(rows, store.AggregateRow{
			BucketMinute: k.BucketMinute,
			ProcessName:  k.ProcessName,
			ProcessPath:  k.ProcessPath,
			Host:         k.Host,
			DestIP:       k.DestIP,
			Outbound:     k.Outbound,
			Chains:       k.Chains,
			Network:      k.Network,
			Upload:       v.Upload,
			Download:     v.Download,
			ConnCount:    v.ConnCount,
		})
	}
	a.buckets = make(map[bucketKey]*bucketVal)
	a.mu.Unlock()
	return a.st.InsertAggregateBatch(ctx, rows)
}

// RunMaintenance periodically flushes buckets, prunes old rows, and (rarely) vacuums.
// Blocks until ctx is cancelled.
func (a *Aggregator) RunMaintenance(ctx context.Context, retentionDays int) {
	flushTicker := time.NewTicker(15 * time.Second)
	pruneTicker := time.NewTicker(time.Hour)
	vacuumTicker := time.NewTicker(7 * 24 * time.Hour)
	defer flushTicker.Stop()
	defer pruneTicker.Stop()
	defer vacuumTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := a.FlushAll(context.Background()); err != nil {
				log.Printf("aggregator: shutdown flush: %v", err)
			}
			return
		case <-flushTicker.C:
			if err := a.FlushCompleted(ctx); err != nil {
				log.Printf("aggregator: flush: %v", err)
			}
		case <-pruneTicker.C:
			if n, err := a.st.Prune(ctx, retentionDays); err != nil {
				log.Printf("aggregator: prune: %v", err)
			} else if n > 0 {
				log.Printf("aggregator: pruned %d rows", n)
			}
		case <-vacuumTicker.C:
			if err := a.st.Vacuum(ctx); err != nil {
				log.Printf("aggregator: vacuum: %v", err)
			}
		}
	}
}
