package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/radiergummi/cetacean/internal/api"
	promapi "github.com/radiergummi/cetacean/internal/api/prometheus"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/recommendations"
	"github.com/radiergummi/cetacean/internal/version"
	"tailscale.com/tsnet"
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
		fmt.Printf(
			"cetacean %s (commit %s, built %s)\n",
			version.Version,
			version.Commit,
			version.Date,
		)
		os.Exit(0)
	}

	configPath := flags.Config
	if configPath == "" {
		configPath = config.DiscoverConfigFile()
	}

	fc, err := config.LoadFile(configPath)
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

	if configPath != "" {
		slog.Info("loaded config file", "path", configPath)
	}

	authCfg, err := config.LoadAuth(flags, fc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth configuration error: %v\n", err)
		os.Exit(1)
	}

	tlsCfg := config.LoadTLS(flags, fc)
	if err := config.ValidateTLS(tlsCfg); err != nil {
		fmt.Fprintf(os.Stderr, "TLS configuration error: %v\n", err)
		os.Exit(1)
	}
	if authCfg.Mode == "cert" && !tlsCfg.Enabled() {
		fmt.Fprintf(os.Stderr, "cert auth mode requires CETACEAN_TLS_CERT and CETACEAN_TLS_KEY\n")
		os.Exit(1)
	}

	var authProvider auth.Provider
	var tsnetServer *tsnet.Server
	var tsnetLn net.Listener
	switch authCfg.Mode {
	case "none":
		authProvider = &auth.NoneProvider{}
	case "oidc":
		authProvider, err = auth.NewOIDCProvider(context.Background(), auth.OIDCProviderConfig{
			Issuer:       authCfg.OIDC.Issuer,
			ClientID:     authCfg.OIDC.ClientID,
			ClientSecret: authCfg.OIDC.ClientSecret,
			RedirectURL:  authCfg.OIDC.RedirectURL,
			Scopes:       authCfg.OIDC.Scopes,
			SessionKey:   authCfg.OIDC.SessionKey,
		})
		if err != nil {
			slog.Error("OIDC provider setup failed", "error", err)
			os.Exit(1)
		}
	case "tailscale":
		if authCfg.Tailscale.Mode == "tsnet" {
			authProvider, tsnetServer, tsnetLn, err = auth.NewTailscaleTsnetProvider(
				authCfg.Tailscale.Hostname,
				authCfg.Tailscale.AuthKey,
				authCfg.Tailscale.StateDir,
				authCfg.Tailscale.Capability,
			)
			if err != nil {
				slog.Error("tsnet setup failed", "error", err)
				os.Exit(1)
			}
			defer tsnetServer.Close()
			defer tsnetLn.Close()
		} else {
			authProvider = auth.NewTailscaleLocalProvider(authCfg.Tailscale.Capability)
		}
	case "cert":
		authProvider = &auth.CertProvider{}
	case "headers":
		authProvider = auth.NewHeadersProvider(authCfg.Headers)
	default:
		fmt.Fprintf(os.Stderr, "unknown auth mode %q\n", authCfg.Mode)
		//nolint:gocritic // exitAfterDefer: tsnet defers only run in the tailscale case, not here
		os.Exit(1)
	}

	// SSE broadcaster
	broadcaster := sse.NewBroadcaster(cfg.SSEBatchInterval, api.WriteErrorCode)
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
		//nolint:gosec // DataDir is operator-configured, not user input
		if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
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
	var promClient *promapi.Client
	var metricsProxy *promapi.Proxy
	if cfg.PrometheusURL != "" {
		promClient = promapi.NewClient(cfg.PrometheusURL)
		metricsProxy = promapi.NewProxy(cfg.PrometheusURL, api.WriteErrorCode)
		slog.Info("prometheus configured", "url", cfg.PrometheusURL)
	} else {
		slog.Warn("prometheus not configured, metrics disabled")
	}
	// Recommendations engine
	sizingCfg, err := config.LoadSizing(fc)
	if err != nil {
		slog.Error("failed to load sizing config", "error", err)
		os.Exit(1)
	}

	var checkers []recommendations.Checker
	// Always register cache-only checkers.
	checkers = append(checkers,
		recommendations.NewConfigChecker(stateCache),
		recommendations.NewClusterChecker(stateCache),
	)
	// Register Prometheus-dependent checkers when available.
	if promClient != nil {
		checkers = append(checkers,
			recommendations.NewSizingChecker(promClient.InstantQuery, stateCache, sizingCfg),
			recommendations.NewOperationalChecker(promClient.InstantQuery, stateCache, sizingCfg.Lookback),
		)
	}
	recEngine := recommendations.NewEngine(checkers...)
	if recEngine != nil {
		go recEngine.Run(ctx)
		slog.Info("recommendation engine started", "checkers", len(checkers))
	}

	slog.Info("operations level", "level", cfg.OperationsLevel)
	handlers := api.NewHandlers(
		stateCache,
		broadcaster,
		dockerClient,
		dockerClient,
		dockerClient,
		dockerClient,
		watcher.Ready(),
		promClient,
		cfg.OperationsLevel,
		recEngine,
	)

	// SPA
	distFS, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		slog.Error("failed to create sub FS", "error", err)
		os.Exit(1)
	}
	spa := api.NewSPAHandler(distFS, cfg.BasePath)

	if cfg.Pprof {
		slog.Warn("pprof endpoints enabled", "path", "/debug/pprof/")
	}

	router := api.NewRouter(
		handlers,
		broadcaster,
		metricsProxy,
		spa,
		openapiSpec,
		scalarJS,
		cfg.Pprof,
		authProvider,
		cfg.BasePath,
	)

	var serverTLSConfig *tls.Config
	if authCfg.Mode == "cert" {
		caCert, err := os.ReadFile(filepath.Clean(authCfg.Cert.CA))
		if err != nil {
			slog.Error("failed to read CA cert", "error", err)
			os.Exit(1)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			slog.Error("failed to parse CA cert")
			os.Exit(1)
		}
		serverTLSConfig = &tls.Config{
			ClientCAs:  caPool,
			ClientAuth: tls.RequireAndVerifyClientCert,
		}
	}

	// tsnet mode: dual listeners (tailnet for app, regular for meta only)
	if tsnetLn != nil {
		serveDualListeners(ctx, cfg, tlsCfg, router, handlers, tsnetLn)
		return
	}

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0, // SSE requires no write timeout; per-request timeouts used instead
		IdleTimeout:  120 * time.Second,
		TLSConfig:    serverTLSConfig,
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

	slog.Info(
		"server started",
		"addr",
		cfg.ListenAddr,
		"base_path",
		cfg.BasePath,
		"version",
		version.Version,
		"commit",
		version.Commit,
		"auth",
		authCfg.Mode,
	)
	if tlsCfg.Enabled() {
		slog.Info("TLS enabled", "cert", tlsCfg.Cert, "key", tlsCfg.Key)
		if err := server.ListenAndServeTLS(tlsCfg.Cert, tlsCfg.Key); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	} else {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}
}

func runHealthcheck() int {
	addr := os.Getenv("CETACEAN_LISTEN_ADDR")
	if addr == "" {
		addr = ":9000"
	}
	basePath := config.NormalizeBasePath(os.Getenv("CETACEAN_BASE_PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"http://localhost"+addr+basePath+"/-/ready",
		nil,
	)
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

// serveDualListeners runs two HTTP servers for tsnet mode:
// - the full router on the tsnet listener (tailnet traffic)
// - meta endpoints only on the regular listener (health checks from Docker)
func serveDualListeners(
	ctx context.Context,
	cfg *config.Config,
	tlsCfg config.TLSConfig,
	router http.Handler,
	h *api.Handlers,
	tsnetLn net.Listener,
) {
	metaMux := http.NewServeMux()
	metaMux.HandleFunc("GET /-/health", h.HandleHealth)
	metaMux.HandleFunc("GET /-/ready", h.HandleReady)
	metaMux.HandleFunc("GET /-/metrics/status", h.HandleMonitoringStatus)

	metaServer := &http.Server{
		Addr:        cfg.ListenAddr,
		Handler:     metaMux,
		ReadTimeout: 5 * time.Second,
		IdleTimeout: 120 * time.Second,
	}

	appServer := &http.Server{
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown of both servers
	go func() { //nolint:gosec // G118: context.Background is correct here — ctx is done, we need a fresh timeout
		<-ctx.Done()
		slog.Info("shutting down", "cause", context.Cause(ctx))
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := appServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("tsnet server shutdown error", "error", err)
		}
		if err := metaServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("meta server shutdown error", "error", err)
		}
	}()

	// Start meta server in background
	go func() {
		slog.Info("meta server started", "addr", cfg.ListenAddr)
		if tlsCfg.Enabled() {
			if err := metaServer.ListenAndServeTLS(
				tlsCfg.Cert,
				tlsCfg.Key,
			); err != http.ErrServerClosed {
				slog.Error("meta server error", "error", err)
			}
		} else {
			if err := metaServer.ListenAndServe(); err != http.ErrServerClosed {
				slog.Error("meta server error", "error", err)
			}
		}
	}()

	// Serve full router on tsnet listener (blocking)
	slog.Info("tsnet server started", "auth", "tailscale")
	if err := appServer.Serve(tsnetLn); err != http.ErrServerClosed {
		slog.Error("tsnet server error", "error", err)
		os.Exit(1)
	}
}
