package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/radiergummi/cetacean/internal/api"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/version"
)

//go:embed frontend/dist/*
var frontendDist embed.FS

//go:embed api/openapi.yaml
var openapiSpec []byte

//go:embed api/scalar/standalone.js
var scalarJS []byte

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck())
	}

	flags, err := config.ParseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "flag error: %v\n", err)
		os.Exit(2)
	}

	if flags.Version {
		fmt.Printf("cetacean %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
		os.Exit(0)
	}

	fc, err := config.LoadFile(flags.Config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(fc, flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	// Set up structured logging
	var logHandler slog.Handler
	opts := &slog.HandlerOptions{Level: cfg.SlogLevel()}
	if cfg.LogFormat == "text" {
		logHandler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		logHandler = slog.NewJSONHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(logHandler))

	// SSE broadcaster
	broadcaster := api.NewBroadcaster(cfg.SSEBatchInterval)
	defer broadcaster.Close()

	// State cache — broadcasts changes via SSE
	stateCache := cache.New(func(e cache.Event) {
		broadcaster.Broadcast(e)
	})

	// Docker client + watcher
	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		slog.Error("docker client failed", "error", err)
		os.Exit(1) //nolint:gocritic // defers are trivial cleanup; OS reclaims on exit
	}
	defer dockerClient.Close() //nolint:errcheck // best-effort shutdown close

	snapshotPath := ""
	if cfg.Snapshot {
		snapshotPath = filepath.Join(cfg.DataDir, "snapshot.json")
		if err := os.MkdirAll(cfg.DataDir, 0700); err != nil { //nolint:gosec // DataDir is operator-configured, not user input
			slog.Warn("could not create data dir", "error", err)
		}
		if err := stateCache.LoadFromDisk(snapshotPath); err != nil {
			slog.Info("no snapshot loaded", "error", err)
		} else {
			slog.Info("loaded snapshot from disk", "age", stateCache.SnapshotAge())
		}
	}

	watcher := docker.NewWatcher(dockerClient, stateCache, snapshotPath)

	// Start watcher in background
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go watcher.Run(ctx)

	// API — pass ready channel so /-/ready reports sync status
	var promClient *api.PromClient
	var promProxy http.Handler
	if cfg.PrometheusURL != "" {
		promClient = api.NewPromClient(cfg.PrometheusURL)
		promProxy = api.NewPrometheusProxy(cfg.PrometheusURL)
		slog.Info("prometheus configured", "url", cfg.PrometheusURL)
	} else {
		promProxy = api.PrometheusNotConfiguredHandler()
		slog.Warn("prometheus not configured, metrics disabled")
	}
	handlers := api.NewHandlers(stateCache, broadcaster, dockerClient, dockerClient, watcher.Ready(), promClient)

	// SPA
	distFS, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		slog.Error("failed to create sub FS", "error", err)
		os.Exit(1)
	}
	spa := api.NewSPAHandler(distFS)

	if cfg.Pprof {
		slog.Warn("pprof endpoints enabled", "path", "/debug/pprof/")
	}

	router := api.NewRouter(handlers, broadcaster, promProxy, spa, openapiSpec, scalarJS, cfg.Pprof)

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0, // SSE requires no server-level write timeout; JSON handlers set per-request deadlines
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		slog.Info("shutting down", "cause", context.Cause(ctx))
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	slog.Info("server started", "addr", cfg.ListenAddr, "version", version.Version, "commit", version.Commit)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func runHealthcheck() int {
	addr := os.Getenv("CETACEAN_LISTEN_ADDR")
	if addr == "" {
		addr = ":9000"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost"+addr+"/-/ready", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
		return 1
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
		return 1
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck failed: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
