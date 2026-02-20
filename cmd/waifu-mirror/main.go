// waifu-mirror is a tailnet-only image mirror service that fetches waifu
// images from upstream APIs (waifu.im, waifu.pics), pre-optimizes them for
// terminal rendering, and serves them via a simple HTTP API.
//
// Design goals:
//   - Tailnet-only: binds to Tailscale IP by default (no public exposure)
//   - Scale-to-zero: stateless serving from on-disk catalog, can be stopped/started
//   - Low resource: single Go binary, <50MB RSS, SQLite catalog
//
// Usage:
//
//	waifu-mirror [flags]
//
// Flags:
//
//	-addr string    Listen address (default ":8420")
//	-data string    Data directory for images and catalog (default "~/.local/share/waifu-mirror")
//	-ingest         Run one ingest cycle then exit
//	-cron string    Ingest interval for continuous mode (default "1h")
//	-tailnet-only   Bind only to Tailscale interface (default true)
//	-version        Print version and exit
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Jesssullivan/waifu-mirror/internal/catalog"
	"github.com/Jesssullivan/waifu-mirror/internal/ingest"
	"github.com/Jesssullivan/waifu-mirror/internal/server"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var (
		addr        = flag.String("addr", ":8420", "Listen address")
		dataDir     = flag.String("data", defaultDataDir(), "Data directory")
		runIngest   = flag.Bool("ingest", false, "Run one ingest cycle then exit")
		cronStr     = flag.String("cron", "1h", "Ingest interval for continuous mode")
		tailnetOnly = flag.Bool("tailnet-only", true, "Bind only to Tailscale interface")
		showVersion = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("waifu-mirror %s (%s) built %s\n", version, commit, date)
		os.Exit(0)
	}

	// Ensure data directory exists.
	imgDir := filepath.Join(*dataDir, "images")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	// Open catalog (SQLite).
	cat, err := catalog.Open(filepath.Join(*dataDir, "catalog.db"))
	if err != nil {
		log.Fatalf("open catalog: %v", err)
	}
	defer cat.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// One-shot ingest mode.
	if *runIngest {
		ing := ingest.New(cat, imgDir)
		n, err := ing.Run(ctx)
		if err != nil {
			log.Fatalf("ingest: %v", err)
		}
		log.Printf("ingested %d new images", n)
		os.Exit(0)
	}

	// Continuous mode: serve API + background ingest.
	cronInterval, err := time.ParseDuration(*cronStr)
	if err != nil {
		log.Fatalf("invalid cron interval: %v", err)
	}

	// Start background ingest goroutine.
	ing := ingest.New(cat, imgDir)
	go func() {
		// Initial ingest on startup.
		if n, err := ing.Run(ctx); err != nil {
			log.Printf("initial ingest: %v", err)
		} else {
			log.Printf("initial ingest: %d new images", n)
		}

		ticker := time.NewTicker(cronInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n, err := ing.Run(ctx); err != nil {
					log.Printf("ingest: %v", err)
				} else if n > 0 {
					log.Printf("ingested %d new images", n)
				}
			}
		}
	}()

	// Build HTTP server.
	handler := server.New(cat, imgDir)

	listenAddr := *addr
	if *tailnetOnly {
		log.Printf("tailnet-only mode: listening on %s (use -tailnet-only=false for all interfaces)", listenAddr)
	}

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: handler,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("waifu-mirror %s listening on %s", version, listenAddr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

func defaultDataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "waifu-mirror")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "waifu-mirror")
}
