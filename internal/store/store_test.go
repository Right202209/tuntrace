package store

import (
	"context"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrationsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
}

func TestSettingRoundtrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if v, _ := s.SettingGet(ctx, "x"); v != "" {
		t.Fatalf("expected empty, got %q", v)
	}
	if err := s.SettingSet(ctx, "x", "hello"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SettingGet(ctx, "x"); v != "hello" {
		t.Fatalf("expected hello, got %q", v)
	}
	if err := s.SettingSet(ctx, "x", "world"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SettingGet(ctx, "x"); v != "world" {
		t.Fatalf("expected world, got %q", v)
	}
}

func TestUpsertAggregate(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	row := AggregateRow{
		BucketMinute: 12345,
		ProcessName:  "chrome.exe",
		ProcessPath:  `C:\chrome.exe`,
		Host:         "x.com",
		DestIP:       "1.2.3.4",
		Outbound:     "Auto",
		Chains:       `["Auto"]`,
		Network:      "tcp",
		Upload:       100,
		Download:     200,
		ConnCount:    1,
	}
	if err := s.InsertAggregateBatch(ctx, []AggregateRow{row}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAggregateBatch(ctx, []AggregateRow{row}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.QuerySummary(ctx, "process", 0, 99999, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Upload != 200 || rows[0].Download != 400 || rows[0].Count != 2 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestQuerySummaryInvalidDim(t *testing.T) {
	s := openTemp(t)
	if _, err := s.QuerySummary(context.Background(), "evil; DROP TABLE", 0, 1, 10); err == nil {
		t.Fatal("expected error on invalid dim")
	}
}

func TestMissingProcessRatio(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	rows := []AggregateRow{
		{BucketMinute: 1, ProcessName: "chrome.exe", Upload: 100, Download: 0, ConnCount: 1},
		{BucketMinute: 1, ProcessName: "", Upload: 0, Download: 300, ConnCount: 1},
	}
	if err := s.InsertAggregateBatch(ctx, rows); err != nil {
		t.Fatal(err)
	}
	r, err := s.MissingProcessRatio(ctx, 0, 99999)
	if err != nil {
		t.Fatal(err)
	}
	if r < 0.74 || r > 0.76 {
		t.Fatalf("expected ~0.75, got %v", r)
	}
}
