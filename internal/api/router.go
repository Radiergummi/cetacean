package api

import (
	"net/http"
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

	// Services
	mux.HandleFunc("GET /api/services", h.HandleListServices)
	mux.HandleFunc("GET /api/services/{id}", h.HandleGetService)

	// Tasks
	mux.HandleFunc("GET /api/tasks", h.HandleListTasks)
	mux.HandleFunc("GET /api/tasks/{id}", h.HandleGetTask)

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

	// SPA fallback (must be last)
	mux.Handle("/", spa)

	return mux
}
