package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cetacean/internal/api"
	"cetacean/internal/cache"
	"cetacean/internal/config"
	"cetacean/internal/docker"
)

//go:embed frontend/dist/*
var frontendDist embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	// SSE broadcaster
	broadcaster := api.NewBroadcaster()
	defer broadcaster.Close()

	// State cache — broadcasts changes via SSE
	stateCache := cache.New(func(e cache.Event) {
		broadcaster.Broadcast(e)
	})

	// Docker client + watcher
	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		log.Fatalf("docker client error: %v", err)
	}
	defer dockerClient.Close() //nolint:errcheck // best-effort shutdown close

	watcher := docker.NewWatcher(dockerClient, stateCache)

	// Start watcher in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go watcher.Run(ctx)

	// API — pass ready channel so /api/ready reports sync status
	handlers := api.NewHandlers(stateCache, dockerClient, watcher.Ready())
	promProxy := api.NewPrometheusProxy(cfg.PrometheusURL)

	// SPA
	distFS, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		log.Fatalf("failed to create sub FS: %v", err)
	}
	spa := api.NewSPAHandler(distFS)

	router := api.NewRouter(handlers, broadcaster, promProxy, spa)

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0, // SSE requires no write timeout; per-request timeouts used instead
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	log.Printf("cetacean listening on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
