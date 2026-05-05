package collector

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"sync"
	"time"

	"tuntrace/internal/clash"
)

// Sink consumes per-poll connection deltas. Implemented by aggregator.
type Sink interface {
	IngestDeltas(ctx context.Context, deltas []Delta, observedAt time.Time) error
}

// Fetcher is satisfied by *clash.Client; an interface lets tests inject a fake.
type Fetcher interface {
	FetchConnections(ctx context.Context) (*clash.ConnectionsResponse, error)
}

// Delta is the byte-count growth of a single live connection between polls.
// (Upload + Download will both be > 0 only if the connection was active in the window.)
type Delta struct {
	ConnID      string
	Upload      int64
	Download    int64
	ProcessName string
	ProcessPath string
	Host        string
	DestIP      string
	Outbound    string
	Chains      []string
	ChainsJSON  string
	Network     string
}

// LiveSnapshot is what GET /api/connections/live returns. It mirrors the latest
// observed mihomo connections enriched with the most recent per-conn deltas.
type LiveSnapshot struct {
	ObservedAt int64           `json:"observedAt"`
	Items      []LiveSnapItem `json:"items"`
}

type LiveSnapItem struct {
	ID          string   `json:"id"`
	ProcessName string   `json:"processName"`
	ProcessPath string   `json:"processPath"`
	Host        string   `json:"host"`
	DestIP      string   `json:"destIP"`
	Network     string   `json:"network"`
	Outbound    string   `json:"outbound"`
	Chains      []string `json:"chains"`
	Upload      int64    `json:"upload"`
	Download    int64    `json:"download"`
}

type Poller struct {
	fetcher  Fetcher
	sink     Sink
	interval time.Duration
	now      func() time.Time

	mu                sync.Mutex
	prev              map[string]clash.Connection
	lastUploadTotal   int64
	lastDownloadTotal int64
	lastSnapshot      LiveSnapshot
}

func NewPoller(f Fetcher, sink Sink, interval time.Duration) *Poller {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Poller{
		fetcher:  f,
		sink:     sink,
		interval: interval,
		prev:     make(map[string]clash.Connection),
		now:      time.Now,
	}
}

// Run blocks until ctx is cancelled. Errors are logged and retried on the next tick.
func (p *Poller) Run(ctx context.Context) {
	t := time.NewTicker(p.interval)
	defer t.Stop()
	if err := p.tick(ctx); err != nil {
		log.Printf("collector: first tick: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.tick(ctx); err != nil {
				log.Printf("collector: tick: %v", err)
			}
		}
	}
}

func (p *Poller) tick(ctx context.Context) error {
	resp, err := p.fetcher.FetchConnections(ctx)
	if err != nil {
		return err
	}
	deltas, snap := p.process(resp)
	p.mu.Lock()
	p.lastSnapshot = snap
	p.mu.Unlock()
	if len(deltas) == 0 {
		return nil
	}
	return p.sink.IngestDeltas(ctx, deltas, p.now())
}

// process is split out so tests can drive it deterministically without HTTP.
func (p *Poller) process(resp *clash.ConnectionsResponse) ([]Delta, LiveSnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// counter-reset: mihomo restarted or counters wrapped → drop baselines, treat
	// every current upload/download as fresh growth so we don't emit huge negatives.
	if resp.UploadTotal < p.lastUploadTotal || resp.DownloadTotal < p.lastDownloadTotal {
		log.Printf("collector: detected mihomo counter reset, clearing baselines")
		p.prev = make(map[string]clash.Connection)
	}
	p.lastUploadTotal = resp.UploadTotal
	p.lastDownloadTotal = resp.DownloadTotal

	deltas := make([]Delta, 0, len(resp.Connections))
	snapItems := make([]LiveSnapItem, 0, len(resp.Connections))
	active := make(map[string]struct{}, len(resp.Connections))

	for i := range resp.Connections {
		c := resp.Connections[i]
		active[c.ID] = struct{}{}

		var dUp, dDown int64
		if prev, ok := p.prev[c.ID]; ok {
			dUp = c.Upload - prev.Upload
			dDown = c.Download - prev.Download
			if dUp < 0 {
				dUp = c.Upload
			}
			if dDown < 0 {
				dDown = c.Download
			}
		} else {
			dUp = c.Upload
			dDown = c.Download
		}

		p.prev[c.ID] = c

		chains := append([]string(nil), c.Chains...)
		chainsJSON, _ := json.Marshal(chains)
		outbound := outboundOf(chains)

		snapItems = append(snapItems, LiveSnapItem{
			ID:          c.ID,
			ProcessName: c.Metadata.Process,
			ProcessPath: c.Metadata.ProcessPath,
			Host:        firstNonEmpty(c.Metadata.Host, c.Metadata.DestinationIP),
			DestIP:      c.Metadata.DestinationIP,
			Network:     c.Metadata.Network,
			Outbound:    outbound,
			Chains:      chains,
			Upload:      c.Upload,
			Download:    c.Download,
		})

		if dUp == 0 && dDown == 0 {
			continue
		}

		deltas = append(deltas, Delta{
			ConnID:      c.ID,
			Upload:      dUp,
			Download:    dDown,
			ProcessName: c.Metadata.Process,
			ProcessPath: c.Metadata.ProcessPath,
			Host:        firstNonEmpty(c.Metadata.Host, c.Metadata.DestinationIP),
			DestIP:      c.Metadata.DestinationIP,
			Outbound:    outbound,
			Chains:      chains,
			ChainsJSON:  string(chainsJSON),
			Network:     c.Metadata.Network,
		})
	}

	for id := range p.prev {
		if _, ok := active[id]; !ok {
			delete(p.prev, id)
		}
	}

	sort.Slice(snapItems, func(i, j int) bool {
		return (snapItems[i].Upload + snapItems[i].Download) > (snapItems[j].Upload + snapItems[j].Download)
	})

	snap := LiveSnapshot{ObservedAt: p.now().UnixMilli(), Items: snapItems}
	return deltas, snap
}

// Snapshot returns the most recent observed connection list. Safe for HTTP handlers.
func (p *Poller) Snapshot() LiveSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.lastSnapshot
	out.Items = append([]LiveSnapItem(nil), p.lastSnapshot.Items...)
	return out
}

func outboundOf(chains []string) string {
	if len(chains) == 0 {
		return "DIRECT"
	}
	// mihomo's chain order is [last-applied, ..., first-applied]; the first
	// element is the outermost proxy/group, which is what users usually see in UIs.
	return chains[0]
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
