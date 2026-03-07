package api

import (
	"net/http"
	"net/http/pprof"
)

func NewRouter(h *Handlers, b *Broadcaster, promProxy http.Handler, spa http.Handler) http.Handler {
	mux := http.NewServeMux()

	// SSE
	mux.Handle("GET /api/events", b)

	// Cluster
	mux.HandleFunc("GET /api/cluster", h.HandleCluster)

	// Nodes
	mux.HandleFunc("GET /api/nodes", h.HandleListNodes)
	mux.HandleFunc("GET /api/nodes/{id}", h.HandleGetNode)
	mux.HandleFunc("GET /api/nodes/{id}/tasks", h.HandleNodeTasks)

	// Services
	mux.HandleFunc("GET /api/services", h.HandleListServices)
	mux.HandleFunc("GET /api/services/{id}", h.HandleGetService)
	mux.HandleFunc("GET /api/services/{id}/tasks", h.HandleServiceTasks)
	mux.HandleFunc("GET /api/services/{id}/logs", h.HandleServiceLogs)

	// Tasks
	mux.HandleFunc("GET /api/tasks", h.HandleListTasks)
	mux.HandleFunc("GET /api/tasks/{id}", h.HandleGetTask)
	mux.HandleFunc("GET /api/tasks/{id}/logs", h.HandleTaskLogs)

	// Stacks
	mux.HandleFunc("GET /api/stacks", h.HandleListStacks)
	mux.HandleFunc("GET /api/stacks/{name}", h.HandleGetStack)

	// Configs
	mux.HandleFunc("GET /api/configs", h.HandleListConfigs)

	// Secrets
	mux.HandleFunc("GET /api/secrets", h.HandleListSecrets)

	// Networks
	mux.HandleFunc("GET /api/networks", h.HandleListNetworks)

	// Volumes
	mux.HandleFunc("GET /api/volumes", h.HandleListVolumes)

	// Prometheus proxy
	mux.Handle("GET /api/metrics/", promProxy)

	// Profiling
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)

	// SPA fallback (must be last)
	mux.Handle("/", spa)

	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
