package web

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"tuntrace/internal/collector"
	"tuntrace/internal/settings"
	"tuntrace/internal/store"
)

type Server struct {
	addr     string
	store    *store.Store
	settings *settings.Manager
	poller   *collector.Poller
}

func NewServer(addr string, st *store.Store, sm *settings.Manager, p *collector.Poller) *Server {
	return &Server{addr: addr, store: st, settings: sm, poller: p}
}

func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	dist, err := distSub()
	if err != nil {
		return err
	}
	mux.Handle("/", http.FileServer(http.FS(dist)))

	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/settings/mihomo", s.handleMihomoSettings)
	mux.HandleFunc("/api/summary", s.handleSummary)
	mux.HandleFunc("/api/timeseries", s.handleTimeseries)
	mux.HandleFunc("/api/connections/live", s.handleLive)
	mux.HandleFunc("/api/diagnostics", s.handleDiagnostics)

	srv := &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Printf("web: listening on %s", s.addr)

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		if err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ts": time.Now().Unix()})
}

func (s *Server) handleMihomoSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.settings.Get())
	case http.MethodPost:
		var body settings.Mihomo
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.settings.Save(r.Context(), body); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, s.settings.Get())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dim := q.Get("group")
	if dim == "" {
		dim = "process"
	}
	from, to := parseRange(q)
	top, _ := strconv.Atoi(q.Get("top"))
	rows, err := s.store.QuerySummary(r.Context(), dim, from, to, top)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group": dim,
		"from":  from,
		"to":    to,
		"rows":  rows,
	})
}

func (s *Server) handleTimeseries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dim := q.Get("dim")
	val := q.Get("value")
	if dim == "" || val == "" {
		http.Error(w, "dim and value required", http.StatusBadRequest)
		return
	}
	from, to := parseRange(q)
	pts, err := s.store.QueryTimeseries(r.Context(), dim, val, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dim":    dim,
		"value":  val,
		"from":   from,
		"to":     to,
		"points": pts,
	})
}

func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.poller.Snapshot())
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix() / 60
	from := now - 5
	ratio, err := s.store.MissingProcessRatio(r.Context(), from, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"missingProcessRatio": ratio,
		"windowMinutes":       5,
		"hint":                `Set "find-process-mode: always" in your mihomo profile to enable per-process attribution.`,
	})
}

// parseRange returns [fromMinute, toMinute] in unix-minute units. Defaults to last 60 min.
func parseRange(q map[string][]string) (int64, int64) {
	now := time.Now().Unix() / 60
	from := now - 60
	to := now
	if v := first(q["from"]); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			from = n
		}
	}
	if v := first(q["to"]); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			to = n
		}
	}
	return from, to
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
