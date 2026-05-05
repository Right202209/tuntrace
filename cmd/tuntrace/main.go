package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"tuntrace/internal/aggregator"
	"tuntrace/internal/clash"
	"tuntrace/internal/collector"
	"tuntrace/internal/settings"
	"tuntrace/internal/store"
	"tuntrace/internal/web"
)

func main() {
	addr := flag.String("listen", envOr("TUNTRACE_LISTEN", ":8080"), "HTTP listen address")
	dbPath := flag.String("db", envOr("TUNTRACE_DB", defaultDBPath()), "SQLite database file")
	pollInterval := flag.Duration("poll", 5*time.Second, "mihomo /connections poll interval")
	retention := flag.Int("retention-days", 30, "days of aggregated data to keep")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	sm := settings.NewManager(st)
	if _, err := sm.Load(context.Background()); err != nil {
		log.Fatalf("load settings: %v", err)
	}

	clashClient := clash.NewClient(func() (string, string) {
		s := sm.Get()
		return s.URL, s.Secret
	})
	agg := aggregator.New(st)
	poller := collector.NewPoller(clashClient, agg, *pollInterval)
	srv := web.NewServer(*addr, st, sm, poller)

	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go poller.Run(rootCtx)
	go agg.RunMaintenance(rootCtx, *retention)
	go func() {
		if err := srv.Run(rootCtx); err != nil {
			log.Printf("web: %v", err)
			cancel()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("shutting down")
	cancel()
	// Give goroutines a moment to flush before main exits.
	time.Sleep(500 * time.Millisecond)
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func defaultDBPath() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "/data/tuntrace.db"
	}
	return filepath.Join("data", "tuntrace.db")
}
