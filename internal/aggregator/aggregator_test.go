package aggregator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"tuntrace/internal/collector"
	"tuntrace/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "agg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mkDelta(proc, host string, up, down int64) collector.Delta {
	return collector.Delta{
		ProcessName: proc,
		ProcessPath: `C:\` + proc,
		Host:        host,
		DestIP:      "1.2.3.4",
		Outbound:    "Auto",
		Chains:      []string{"Auto"},
		ChainsJSON:  `["Auto"]`,
		Network:     "tcp",
		Upload:      up,
		Download:    down,
	}
}

func TestAggregatorMergesSameKeyWithinMinute(t *testing.T) {
	st := openStore(t)
	a := New(st)
	ctx := context.Background()
	at := time.Unix(1_700_000_010, 0) // bucket = 28333333
	a.IngestDeltas(ctx, []collector.Delta{mkDelta("chrome.exe", "x.com", 10, 20)}, at)
	a.IngestDeltas(ctx, []collector.Delta{mkDelta("chrome.exe", "x.com", 5, 7)}, at.Add(20*time.Second))
	a.SetNow(func() time.Time { return at.Add(2 * time.Minute) })
	if err := a.FlushCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.QuerySummary(ctx, "process", 0, 99_999_999, 10)
	if len(rows) != 1 || rows[0].Upload != 15 || rows[0].Download != 27 || rows[0].Count != 2 {
		t.Fatalf("unexpected: %+v", rows)
	}
}

func TestAggregatorOnlyFlushesCompletedBuckets(t *testing.T) {
	st := openStore(t)
	a := New(st)
	ctx := context.Background()
	t1 := time.Unix(1_700_000_010, 0) // minute 28333333
	t2 := time.Unix(1_700_000_080, 0) // minute 28333334 (in progress)
	a.IngestDeltas(ctx, []collector.Delta{mkDelta("chrome.exe", "x.com", 10, 20)}, t1)
	a.IngestDeltas(ctx, []collector.Delta{mkDelta("chrome.exe", "x.com", 99, 99)}, t2)
	// Wall clock is "still in" minute 28333334
	a.SetNow(func() time.Time { return t2.Add(15 * time.Second) })
	if err := a.FlushCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.QuerySummary(ctx, "process", 0, 99_999_999, 10)
	if len(rows) != 1 || rows[0].Upload != 10 || rows[0].Download != 20 {
		t.Fatalf("expected only completed bucket flushed, got %+v", rows)
	}
	// In-progress bucket still in memory:
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.buckets) != 1 {
		t.Fatalf("expected 1 in-memory bucket, got %d", len(a.buckets))
	}
}

func TestAggregatorFlushAll(t *testing.T) {
	st := openStore(t)
	a := New(st)
	ctx := context.Background()
	at := time.Unix(1_700_000_010, 0)
	a.IngestDeltas(ctx, []collector.Delta{mkDelta("chrome.exe", "x.com", 10, 20)}, at)
	a.SetNow(func() time.Time { return at })
	if err := a.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.QuerySummary(ctx, "process", 0, 99_999_999, 10)
	if len(rows) != 1 {
		t.Fatalf("FlushAll should write in-progress bucket, got %+v", rows)
	}
}
