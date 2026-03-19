package api

import (
	"net/http"
	"net/http/pprof"

	"github.com/radiergummi/cetacean/internal/auth"
)

func NewRouter(h *Handlers, b *Broadcaster, promProxy http.Handler, metricsProxy *PrometheusProxy, spa http.Handler, openapiSpec []byte, scalarJS []byte, enablePprof bool, authProvider auth.Provider) http.Handler {
	mux := http.NewServeMux()

	authProvider.RegisterRoutes(mux)

	// Meta endpoints (no content negotiation, no discovery links)
	mux.HandleFunc("GET /-/health", h.HandleHealth)
	mux.HandleFunc("GET /-/ready", h.HandleReady)
	mux.HandleFunc("GET /-/metrics/status", h.HandleMonitoringStatus)
	mux.HandleFunc("GET /-/metrics/labels", metricsProxy.HandleMetricsLabels)
	mux.HandleFunc("GET /-/metrics/labels/{name}", metricsProxy.HandleMetricsLabelValues)
	mux.HandleFunc("GET /-/metrics/query_range", contentNegotiatedWithSSE(
		promProxy.ServeHTTP,
		h.HandleMetricsStream,
		spa,
	))
	mux.Handle("GET /-/metrics/", promProxy)

	// Metrics (content-negotiated: JSON → proxy, SSE → stream, HTML → SPA)
	mux.HandleFunc("GET /metrics", contentNegotiatedWithSSE(
		metricsProxy.HandleMetrics,
		h.HandleMetricsStream,
		spa,
	))

	// API documentation (content-negotiated)
	mux.HandleFunc("GET /api", HandleAPIDoc(openapiSpec))
	mux.HandleFunc("GET /api/scalar.js", HandleScalarJS(scalarJS))
	mux.HandleFunc("GET /api/context.jsonld", HandleContext)

	// SSE events
	mux.Handle("GET /events", sseOnly(b, spa))

	// Cluster
	mux.HandleFunc("GET /cluster", contentNegotiated(h.HandleCluster, spa))
	mux.HandleFunc("GET /cluster/metrics", contentNegotiated(h.HandleClusterMetrics, spa))
	mux.HandleFunc("GET /cluster/capacity", contentNegotiated(h.HandleClusterCapacity, spa))
	mux.HandleFunc("GET /swarm", contentNegotiated(h.HandleSwarm, spa))
	mux.HandleFunc("GET /disk-usage", contentNegotiated(h.HandleDiskUsage, spa))
	mux.HandleFunc("GET /plugins", contentNegotiated(h.HandlePlugins, spa))

	// Nodes
	mux.HandleFunc("GET /nodes", contentNegotiatedWithSSE(h.HandleListNodes, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "node") }, spa))
	mux.HandleFunc("GET /nodes/{id}", contentNegotiatedWithSSE(h.HandleGetNode, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "node", r.PathValue("id")) }, spa))
	mux.HandleFunc("GET /nodes/{id}/tasks", contentNegotiated(h.HandleNodeTasks, spa))

	// Services
	mux.HandleFunc("GET /services", contentNegotiatedWithSSE(h.HandleListServices, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "service") }, spa))
	mux.HandleFunc("GET /services/{id}", contentNegotiatedWithSSE(h.HandleGetService, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "service", r.PathValue("id")) }, spa))
	mux.HandleFunc("GET /services/{id}/tasks", contentNegotiated(h.HandleServiceTasks, spa))
	mux.HandleFunc("GET /services/{id}/logs", contentNegotiatedWithSSE(h.HandleServiceLogs, h.HandleServiceLogs, spa))

	// Node write operations
	mux.Handle("PUT /nodes/{id}/availability", requireWrite(h.HandleUpdateNodeAvailability))
	mux.HandleFunc("GET /nodes/{id}/labels", contentNegotiated(h.HandleGetNodeLabels, spa))
	mux.Handle("PATCH /nodes/{id}/labels", requireWrite(h.HandlePatchNodeLabels))

	// Service write operations
	mux.Handle("PUT /services/{id}/scale", requireWrite(h.HandleScaleService))
	mux.Handle("PUT /services/{id}/image", requireWrite(h.HandleUpdateServiceImage))
	mux.Handle("POST /services/{id}/rollback", requireWrite(h.HandleRollbackService))
	mux.Handle("POST /services/{id}/restart", requireWrite(h.HandleRestartService))
	mux.HandleFunc("GET /services/{id}/env", contentNegotiated(h.HandleGetServiceEnv, spa))
	mux.Handle("PATCH /services/{id}/env", requireWrite(h.HandlePatchServiceEnv))
	mux.HandleFunc("GET /services/{id}/resources", contentNegotiated(h.HandleGetServiceResources, spa))
	mux.Handle("PATCH /services/{id}/resources", requireWrite(h.HandlePatchServiceResources))

	// Tasks
	mux.HandleFunc("GET /tasks", contentNegotiatedWithSSE(h.HandleListTasks, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "task") }, spa))
	mux.HandleFunc("GET /tasks/{id}", contentNegotiatedWithSSE(h.HandleGetTask, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "task", r.PathValue("id")) }, spa))
	mux.HandleFunc("GET /tasks/{id}/logs", contentNegotiatedWithSSE(h.HandleTaskLogs, h.HandleTaskLogs, spa))
	mux.Handle("DELETE /tasks/{id}", requireWrite(h.HandleRemoveTask))

	// History
	mux.HandleFunc("GET /history", contentNegotiated(h.HandleHistory, spa))

	// Stacks
	mux.HandleFunc("GET /stacks", contentNegotiatedWithSSE(h.HandleListStacks, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "stack") }, spa))
	mux.HandleFunc("GET /stacks/summary", contentNegotiated(h.HandleStackSummary, spa))
	mux.HandleFunc("GET /stacks/{name}", contentNegotiatedWithSSE(h.HandleGetStack, func(w http.ResponseWriter, r *http.Request) {
		h.broadcaster.serveSSE(w, r, stackMatcher(h.cache, r.PathValue("name")))
	}, spa))

	// Configs
	mux.HandleFunc("GET /configs", contentNegotiatedWithSSE(h.HandleListConfigs, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "config") }, spa))
	mux.HandleFunc("GET /configs/{id}", contentNegotiatedWithSSE(h.HandleGetConfig, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "config", r.PathValue("id")) }, spa))

	// Secrets
	mux.HandleFunc("GET /secrets", contentNegotiatedWithSSE(h.HandleListSecrets, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "secret") }, spa))
	mux.HandleFunc("GET /secrets/{id}", contentNegotiatedWithSSE(h.HandleGetSecret, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "secret", r.PathValue("id")) }, spa))

	// Networks
	mux.HandleFunc("GET /networks", contentNegotiatedWithSSE(h.HandleListNetworks, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "network") }, spa))
	mux.HandleFunc("GET /networks/{id}", contentNegotiatedWithSSE(h.HandleGetNetwork, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "network", r.PathValue("id")) }, spa))

	// Volumes
	mux.HandleFunc("GET /volumes", contentNegotiatedWithSSE(h.HandleListVolumes, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "volume") }, spa))
	mux.HandleFunc("GET /volumes/{name}", contentNegotiatedWithSSE(h.HandleGetVolume, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "volume", r.PathValue("name")) }, spa))

	// Search
	mux.HandleFunc("GET /search", contentNegotiated(h.HandleSearch, spa))

	// Profile
	mux.HandleFunc("GET /profile", contentNegotiated(HandleProfile, spa))

	// Topology
	mux.HandleFunc("GET /topology/networks", contentNegotiated(h.HandleNetworkTopology, spa))
	mux.HandleFunc("GET /topology/placement", contentNegotiated(h.HandlePlacementTopology, spa))

	// Profiling (opt-in via CETACEAN_PPROF=true)
	if enablePprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	// SPA fallback (must be last)
	mux.Handle("/", spa)

	var handler http.Handler = mux
	handler = requestLogger(handler)
	handler = discoveryLinks(handler)
	handler = negotiate(handler)
	handler = auth.Middleware(authProvider)(handler)
	handler = securityHeaders(handler)
	handler = recovery(handler)
	handler = requestID(handler)
	return handler
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https:")
		next.ServeHTTP(w, r)
	})
}
