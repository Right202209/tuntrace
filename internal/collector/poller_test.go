package collector

import (
	"context"
	"testing"
	"time"

	"tuntrace/internal/clash"
)

type fakeSink struct {
	last []Delta
	at   time.Time
}

func (f *fakeSink) IngestDeltas(_ context.Context, deltas []Delta, at time.Time) error {
	f.last = append([]Delta(nil), deltas...)
	f.at = at
	return nil
}

func mkConn(id string, up, down int64, proc, host string) clash.Connection {
	return clash.Connection{
		ID:       id,
		Upload:   up,
		Download: down,
		Chains:   []string{"Auto", "Proxy"},
		Metadata: clash.Metadata{
			Network: "tcp", SourceIP: "127.0.0.1",
			DestinationIP: "1.2.3.4", Host: host, Process: proc,
		},
	}
}

func TestProcessNewConnectionEmitsFullCounters(t *testing.T) {
	p := NewPoller(nil, &fakeSink{}, time.Second)
	resp := &clash.ConnectionsResponse{
		UploadTotal: 100, DownloadTotal: 200,
		Connections: []clash.Connection{mkConn("c1", 100, 200, "chrome.exe", "x.com")},
	}
	deltas, _ := p.process(resp)
	if len(deltas) != 1 || deltas[0].Upload != 100 || deltas[0].Download != 200 {
		t.Fatalf("want full counters, got %+v", deltas)
	}
}

func TestProcessIncrementalDelta(t *testing.T) {
	p := NewPoller(nil, &fakeSink{}, time.Second)
	r1 := &clash.ConnectionsResponse{
		UploadTotal: 100, DownloadTotal: 200,
		Connections: []clash.Connection{mkConn("c1", 100, 200, "chrome.exe", "x.com")},
	}
	p.process(r1)
	r2 := &clash.ConnectionsResponse{
		UploadTotal: 150, DownloadTotal: 350,
		Connections: []clash.Connection{mkConn("c1", 150, 350, "chrome.exe", "x.com")},
	}
	deltas, _ := p.process(r2)
	if len(deltas) != 1 || deltas[0].Upload != 50 || deltas[0].Download != 150 {
		t.Fatalf("want delta 50/150, got %+v", deltas)
	}
}

func TestProcessZeroDeltaNotEmitted(t *testing.T) {
	p := NewPoller(nil, &fakeSink{}, time.Second)
	r1 := &clash.ConnectionsResponse{
		UploadTotal: 100, DownloadTotal: 200,
		Connections: []clash.Connection{mkConn("c1", 100, 200, "chrome.exe", "x.com")},
	}
	p.process(r1)
	deltas, _ := p.process(r1)
	if len(deltas) != 0 {
		t.Fatalf("want no deltas, got %+v", deltas)
	}
}

func TestProcessCounterReset(t *testing.T) {
	p := NewPoller(nil, &fakeSink{}, time.Second)
	r1 := &clash.ConnectionsResponse{
		UploadTotal: 1000, DownloadTotal: 2000,
		Connections: []clash.Connection{mkConn("c1", 1000, 2000, "chrome.exe", "x.com")},
	}
	p.process(r1)
	// mihomo restart: totals dropped, conn IDs are different too
	r2 := &clash.ConnectionsResponse{
		UploadTotal: 30, DownloadTotal: 40,
		Connections: []clash.Connection{mkConn("c2", 30, 40, "chrome.exe", "x.com")},
	}
	deltas, _ := p.process(r2)
	if len(deltas) != 1 || deltas[0].Upload != 30 || deltas[0].Download != 40 {
		t.Fatalf("want counter-reset → emit 30/40 fresh, got %+v", deltas)
	}
}

func TestProcessDisappearedConnectionPurged(t *testing.T) {
	p := NewPoller(nil, &fakeSink{}, time.Second)
	r1 := &clash.ConnectionsResponse{
		UploadTotal: 100, DownloadTotal: 200,
		Connections: []clash.Connection{mkConn("c1", 100, 200, "chrome.exe", "x.com")},
	}
	p.process(r1)
	r2 := &clash.ConnectionsResponse{
		UploadTotal: 100, DownloadTotal: 200,
		Connections: []clash.Connection{mkConn("c2", 5, 5, "vlc.exe", "y.com")},
	}
	p.process(r2)
	if _, ok := p.prev["c1"]; ok {
		t.Fatal("c1 should be purged from prev map")
	}
	if _, ok := p.prev["c2"]; !ok {
		t.Fatal("c2 should be in prev map")
	}
}

func TestProcessSameConnGrowingDecreaseTreatedAsCurrent(t *testing.T) {
	// Edge: per-conn upload counter went down but grand totals didn't.
	// Treat as fresh — emit current value, not negative.
	p := NewPoller(nil, &fakeSink{}, time.Second)
	r1 := &clash.ConnectionsResponse{
		UploadTotal: 100, DownloadTotal: 200,
		Connections: []clash.Connection{mkConn("c1", 100, 200, "chrome.exe", "x.com")},
	}
	p.process(r1)
	r2 := &clash.ConnectionsResponse{
		UploadTotal: 100, DownloadTotal: 200,
		Connections: []clash.Connection{mkConn("c1", 30, 40, "chrome.exe", "x.com")},
	}
	deltas, _ := p.process(r2)
	if len(deltas) != 1 || deltas[0].Upload != 30 || deltas[0].Download != 40 {
		t.Fatalf("want fallback to current 30/40, got %+v", deltas)
	}
}
