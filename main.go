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
	defer dockerClient.Close()

	watcher := docker.NewWatcher(dockerClient, stateCache)

	// Start watcher in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go watcher.Run(ctx)

	// Wait for initial sync
	<-watcher.Ready()
	log.Println("initial sync complete, starting HTTP server")

	// API
	handlers := api.NewHandlers(stateCache)
	promProxy := api.NewPrometheusProxy(cfg.PrometheusURL)

	// SPA
	distFS, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		log.Fatalf("failed to create sub FS: %v", err)
	}
	spa := api.NewSPAHandler(distFS)

	router := api.NewRouter(handlers, broadcaster, promProxy, spa)

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: router,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		cancel()
		server.Close()
	}()

	log.Printf("cetacean listening on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
