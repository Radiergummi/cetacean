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

	"cetacean/internal/api"
	"cetacean/internal/cache"
	"cetacean/internal/config"
	"cetacean/internal/docker"
	"cetacean/internal/notify"
	"cetacean/internal/version"
)

//go:embed frontend/dist/*
var frontendDist embed.FS

func main() {
	cfg, err := config.Load()
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

	// Notification webhooks (optional)
	var notifier *notify.Notifier
	if cfg.NotificationsFile != "" {
		rules, err := notify.LoadRules(cfg.NotificationsFile)
		if err != nil {
			slog.Error("failed to load notification rules", "error", err)
			os.Exit(1)
		}
		if len(rules) > 0 {
			slog.Info("loaded notification rules", "count", len(rules))
			notifier = notify.New(rules)
		}
	}

	// State cache — broadcasts changes via SSE
	stateCache := cache.New(func(e cache.Event) {
		broadcaster.Broadcast(e)
		if notifier != nil {
			notifier.HandleEvent(e, cache.ExtractName(e))
		}
	})

	// Docker client + watcher
	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		slog.Error("docker client failed", "error", err)
		os.Exit(1)
	}
	defer dockerClient.Close() //nolint:errcheck // best-effort shutdown close

	snapshotPath := ""
	if cfg.Snapshot {
		snapshotPath = filepath.Join(cfg.DataDir, "snapshot.json")
		if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
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

	// API — pass ready channel so /api/ready reports sync status
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
	handlers := api.NewHandlers(stateCache, dockerClient, dockerClient, watcher.Ready(), notifier, promClient)

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

	router := api.NewRouter(handlers, broadcaster, promProxy, spa, cfg.Pprof)

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0, // SSE requires no write timeout; per-request timeouts used instead
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
