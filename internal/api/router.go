package api

import (
	"net/http"
	"net/http/pprof"
)

func NewRouter(h *Handlers, b *Broadcaster, promProxy http.Handler, spa http.Handler, openapiSpec []byte, scalarJS []byte, enablePprof bool) http.Handler {
	mux := http.NewServeMux()

	// Meta endpoints (no content negotiation, no discovery links)
	mux.HandleFunc("GET /-/health", h.HandleHealth)
	mux.HandleFunc("GET /-/ready", h.HandleReady)
	mux.HandleFunc("GET /-/metrics/status", h.HandleMonitoringStatus)
	mux.Handle("GET /-/metrics/", promProxy)

	// API documentation (content-negotiated)
	mux.HandleFunc("GET /api", HandleAPIDoc(openapiSpec))
	mux.HandleFunc("GET /api/scalar.js", HandleScalarJS(scalarJS))
	mux.HandleFunc("GET /api/context.jsonld", HandleContext)

	// SSE events
	mux.Handle("GET /events", sseOnly(b, spa))

	// Cluster
	mux.HandleFunc("GET /cluster", contentNegotiated(h.HandleCluster, spa))
	mux.HandleFunc("GET /cluster/metrics", contentNegotiated(h.HandleClusterMetrics, spa))
	mux.HandleFunc("GET /swarm", contentNegotiated(h.HandleSwarm, spa))
	mux.HandleFunc("GET /disk-usage", contentNegotiated(h.HandleDiskUsage, spa))
	mux.HandleFunc("GET /plugins", contentNegotiated(h.HandlePlugins, spa))

	// Nodes
	mux.HandleFunc("GET /nodes", contentNegotiated(h.HandleListNodes, spa))
	mux.HandleFunc("GET /nodes/{id}", contentNegotiated(h.HandleGetNode, spa))
	mux.HandleFunc("GET /nodes/{id}/tasks", contentNegotiated(h.HandleNodeTasks, spa))

	// Services
	mux.HandleFunc("GET /services", contentNegotiated(h.HandleListServices, spa))
	mux.HandleFunc("GET /services/{id}", contentNegotiated(h.HandleGetService, spa))
	mux.HandleFunc("GET /services/{id}/tasks", contentNegotiated(h.HandleServiceTasks, spa))
	mux.HandleFunc("GET /services/{id}/logs", contentNegotiatedWithSSE(h.HandleServiceLogs, h.HandleServiceLogs, spa))

	// Tasks
	mux.HandleFunc("GET /tasks", contentNegotiated(h.HandleListTasks, spa))
	mux.HandleFunc("GET /tasks/{id}", contentNegotiated(h.HandleGetTask, spa))
	mux.HandleFunc("GET /tasks/{id}/logs", contentNegotiatedWithSSE(h.HandleTaskLogs, h.HandleTaskLogs, spa))

	// History
	mux.HandleFunc("GET /history", contentNegotiated(h.HandleHistory, spa))

	// Stacks
	mux.HandleFunc("GET /stacks", contentNegotiated(h.HandleListStacks, spa))
	mux.HandleFunc("GET /stacks/summary", contentNegotiated(h.HandleStackSummary, spa))
	mux.HandleFunc("GET /stacks/{name}", contentNegotiated(h.HandleGetStack, spa))

	// Configs
	mux.HandleFunc("GET /configs", contentNegotiated(h.HandleListConfigs, spa))
	mux.HandleFunc("GET /configs/{id}", contentNegotiated(h.HandleGetConfig, spa))

	// Secrets
	mux.HandleFunc("GET /secrets", contentNegotiated(h.HandleListSecrets, spa))
	mux.HandleFunc("GET /secrets/{id}", contentNegotiated(h.HandleGetSecret, spa))

	// Networks
	mux.HandleFunc("GET /networks", contentNegotiated(h.HandleListNetworks, spa))
	mux.HandleFunc("GET /networks/{id}", contentNegotiated(h.HandleGetNetwork, spa))

	// Volumes
	mux.HandleFunc("GET /volumes", contentNegotiated(h.HandleListVolumes, spa))
	mux.HandleFunc("GET /volumes/{name}", contentNegotiated(h.HandleGetVolume, spa))

	// Notifications
	mux.HandleFunc("GET /notifications/rules", contentNegotiated(h.HandleNotificationRules, spa))

	// Search
	mux.HandleFunc("GET /search", contentNegotiated(h.HandleSearch, spa))

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
	handler = securityHeaders(handler)
	handler = recovery(handler)
	handler = requestID(handler)
	return handler
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
