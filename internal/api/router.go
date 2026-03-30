package api

import (
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/prometheus"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func NewRouter(
	h *Handlers,
	b *sse.Broadcaster,
	metricsProxy *prometheus.Proxy,
	spa http.Handler,
	openapiSpec []byte,
	scalarJS []byte,
	enablePprof bool,
	authProvider auth.Provider,
) http.Handler {
	mux := http.NewServeMux()

	tier1 := requireLevel(config.OpsOperational, h.operationsLevel)
	tier2 := requireLevel(config.OpsConfiguration, h.operationsLevel)
	tier3 := requireLevel(config.OpsImpactful, h.operationsLevel)

	authProvider.RegisterRoutes(mux)

	// Meta endpoints (no content negotiation, no discovery links)
	mux.HandleFunc("GET /-/health", h.HandleHealth)
	mux.HandleFunc("GET /-/ready", h.HandleReady)
	mux.HandleFunc("GET /-/metrics/status", h.HandleMonitoringStatus)
	mux.HandleFunc("GET /-/docker-latest-version", HandleDockerLatestVersion)
	mux.HandleFunc("GET /-/metrics/labels", metricsProxy.HandleMetricsLabels)
	mux.HandleFunc("GET /-/metrics/labels/{name}", metricsProxy.HandleMetricsLabelValues)
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
	mux.HandleFunc("GET /api/errors", contentNegotiated(HandleErrorIndex, spa))
	mux.HandleFunc("GET /api/errors/{code}", contentNegotiated(HandleErrorDetail, spa))

	// SSE events
	mux.Handle("GET /events", sseOnly(b, spa))

	// Cluster
	mux.HandleFunc("GET /cluster", contentNegotiated(h.HandleCluster, spa))
	mux.HandleFunc("GET /cluster/metrics", contentNegotiated(h.HandleClusterMetrics, spa))
	mux.HandleFunc("GET /cluster/capacity", contentNegotiated(h.HandleClusterCapacity, spa))
	mux.HandleFunc("GET /swarm", contentNegotiated(h.HandleSwarm, spa))
	mux.Handle("PATCH /swarm/orchestration", tier2(h.HandlePatchSwarmOrchestration))
	mux.Handle("PATCH /swarm/raft", tier2(h.HandlePatchSwarmRaft))
	mux.Handle("PATCH /swarm/dispatcher", tier2(h.HandlePatchSwarmDispatcher))
	mux.Handle("PATCH /swarm/ca", tier3(h.HandlePatchSwarmCAConfig))
	mux.Handle("PATCH /swarm/encryption", tier3(h.HandlePatchSwarmEncryption))
	mux.Handle("POST /swarm/rotate-token", tier3(h.HandlePostRotateToken))
	mux.Handle("POST /swarm/rotate-unlock-key", tier3(h.HandlePostRotateUnlockKey))
	mux.Handle("POST /swarm/force-rotate-ca", tier3(h.HandlePostForceRotateCA))
	mux.HandleFunc("GET /swarm/unlock-key", h.HandleGetUnlockKey)
	mux.Handle("POST /swarm/unlock", tier3(h.HandlePostUnlockSwarm))
	mux.HandleFunc("GET /disk-usage", contentNegotiated(h.HandleDiskUsage, spa))
	// Plugins
	mux.HandleFunc("GET /plugins", contentNegotiated(h.HandlePlugins, spa))
	mux.HandleFunc("GET /plugins/{name}", contentNegotiated(h.HandlePlugin, spa))
	mux.HandleFunc("GET /swarm/plugins", contentNegotiated(h.HandlePlugins, spa))
	mux.Handle("POST /plugins/privileges", tier3(h.HandlePluginPrivileges))
	mux.Handle("POST /plugins", tier3(h.HandleInstallPlugin))
	mux.Handle("POST /plugins/{name}/enable", tier2(h.HandleEnablePlugin))
	mux.Handle("POST /plugins/{name}/disable", tier2(h.HandleDisablePlugin))
	mux.Handle("DELETE /plugins/{name}", tier3(h.HandleRemovePlugin))
	mux.Handle("POST /plugins/{name}/upgrade", tier3(h.HandleUpgradePlugin))
	mux.Handle("PATCH /plugins/{name}/settings", tier2(h.HandleConfigurePlugin))

	// Nodes
	mux.HandleFunc(
		"GET /nodes",
		contentNegotiatedWithSSE(
			h.HandleListNodes,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventNode) },
			spa,
		),
	)
	mux.HandleFunc(
		"GET /nodes/{id}",
		contentNegotiatedWithSSE(
			h.HandleGetNode,
			func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, cache.EventNode, r.PathValue("id")) },
			spa,
		),
	)
	mux.HandleFunc("GET /nodes/{id}/tasks", contentNegotiated(h.HandleNodeTasks, spa))

	// Recommendations
	mux.HandleFunc("GET /recommendations", contentNegotiated(h.HandleRecommendations, spa))

	// Services
	mux.HandleFunc(
		"GET /services",
		contentNegotiatedWithSSE(
			h.HandleListServices,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventService) },
			spa,
		),
	)
	mux.HandleFunc(
		"GET /services/{id}",
		contentNegotiatedWithSSE(
			h.HandleGetService,
			func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, cache.EventService, r.PathValue("id")) },
			spa,
		),
	)
	mux.HandleFunc("GET /services/{id}/tasks", contentNegotiated(h.HandleServiceTasks, spa))
	mux.HandleFunc(
		"GET /services/{id}/logs",
		contentNegotiatedWithSSE(h.HandleServiceLogs, h.HandleServiceLogs, spa),
	)

	// Node write operations
	mux.Handle("PUT /nodes/{id}/availability", tier3(h.HandleUpdateNodeAvailability))
	mux.HandleFunc("GET /nodes/{id}/labels", contentNegotiated(h.HandleGetNodeLabels, spa))
	mux.Handle("PATCH /nodes/{id}/labels", tier3(h.HandlePatchNodeLabels))
	mux.HandleFunc("GET /nodes/{id}/role", contentNegotiated(h.HandleGetNodeRole, spa))
	mux.Handle("PUT /nodes/{id}/role", tier3(h.HandleUpdateNodeRole))
	mux.Handle("DELETE /nodes/{id}", tier3(h.HandleRemoveNode))

	// Service write operations — tier 1 (operational)
	mux.Handle("PUT /services/{id}/scale", tier1(h.HandleScaleService))
	mux.Handle("PUT /services/{id}/image", tier1(h.HandleUpdateServiceImage))
	mux.Handle("POST /services/{id}/rollback", tier1(h.HandleRollbackService))
	mux.Handle("POST /services/{id}/restart", tier1(h.HandleRestartService))

	// Service write operations — tier 2 (configuration)
	mux.HandleFunc("GET /services/{id}/env", contentNegotiated(h.HandleGetServiceEnv, spa))
	mux.Handle("PATCH /services/{id}/env", tier2(h.HandlePatchServiceEnv))
	mux.HandleFunc("GET /services/{id}/labels", contentNegotiated(h.HandleGetServiceLabels, spa))
	mux.Handle("PATCH /services/{id}/labels", tier2(h.HandlePatchServiceLabels))
	mux.HandleFunc(
		"GET /services/{id}/resources",
		contentNegotiated(h.HandleGetServiceResources, spa),
	)
	mux.Handle("PATCH /services/{id}/resources", tier2(h.HandlePatchServiceResources))
	mux.HandleFunc(
		"GET /services/{id}/healthcheck",
		contentNegotiated(h.HandleGetServiceHealthcheck, spa),
	)
	mux.Handle("PUT /services/{id}/healthcheck", tier2(h.HandlePutServiceHealthcheck))
	mux.Handle("PATCH /services/{id}/healthcheck", tier2(h.HandlePatchServiceHealthcheck))
	mux.HandleFunc(
		"GET /services/{id}/placement",
		contentNegotiated(h.HandleGetServicePlacement, spa),
	)
	mux.Handle("PUT /services/{id}/placement", tier2(h.HandlePutServicePlacement))
	mux.HandleFunc("GET /services/{id}/ports", contentNegotiated(h.HandleGetServicePorts, spa))
	mux.Handle("PATCH /services/{id}/ports", tier2(h.HandlePatchServicePorts))
	mux.HandleFunc(
		"GET /services/{id}/update-policy",
		contentNegotiated(h.HandleGetServiceUpdatePolicy, spa),
	)
	mux.Handle("PATCH /services/{id}/update-policy", tier2(h.HandlePatchServiceUpdatePolicy))
	mux.HandleFunc(
		"GET /services/{id}/rollback-policy",
		contentNegotiated(h.HandleGetServiceRollbackPolicy, spa),
	)
	mux.Handle("PATCH /services/{id}/rollback-policy", tier2(h.HandlePatchServiceRollbackPolicy))
	mux.HandleFunc(
		"GET /services/{id}/log-driver",
		contentNegotiated(h.HandleGetServiceLogDriver, spa),
	)
	mux.Handle("PATCH /services/{id}/log-driver", tier2(h.HandlePatchServiceLogDriver))
	mux.HandleFunc("GET /services/{id}/configs", contentNegotiated(h.HandleGetServiceConfigs, spa))
	mux.Handle("PATCH /services/{id}/configs", tier2(h.HandlePatchServiceConfigs))
	mux.HandleFunc("GET /services/{id}/secrets", contentNegotiated(h.HandleGetServiceSecrets, spa))
	mux.Handle("PATCH /services/{id}/secrets", tier2(h.HandlePatchServiceSecrets))
	mux.HandleFunc(
		"GET /services/{id}/networks",
		contentNegotiated(h.HandleGetServiceNetworks, spa),
	)
	mux.Handle("PATCH /services/{id}/networks", tier2(h.HandlePatchServiceNetworks))
	mux.HandleFunc("GET /services/{id}/mounts", contentNegotiated(h.HandleGetServiceMounts, spa))
	mux.Handle("PATCH /services/{id}/mounts", tier2(h.HandlePatchServiceMounts))

	mux.HandleFunc(
		"GET /services/{id}/container-config",
		contentNegotiated(h.HandleGetServiceContainerConfig, spa),
	)
	mux.Handle("PATCH /services/{id}/container-config", tier2(h.HandlePatchServiceContainerConfig))

	// Service write operations — tier 3 (impactful)
	mux.Handle("PUT /services/{id}/mode", tier3(h.HandleUpdateServiceMode))
	mux.Handle("PUT /services/{id}/endpoint-mode", tier3(h.HandleUpdateServiceEndpointMode))
	mux.Handle("DELETE /services/{id}", tier3(h.HandleRemoveService))

	// Tasks
	mux.HandleFunc(
		"GET /tasks",
		contentNegotiatedWithSSE(
			h.HandleListTasks,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventTask) },
			spa,
		),
	)
	mux.HandleFunc(
		"GET /tasks/{id}",
		contentNegotiatedWithSSE(
			h.HandleGetTask,
			func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, cache.EventTask, r.PathValue("id")) },
			spa,
		),
	)
	mux.HandleFunc(
		"GET /tasks/{id}/logs",
		contentNegotiatedWithSSE(h.HandleTaskLogs, h.HandleTaskLogs, spa),
	)
	mux.Handle("DELETE /tasks/{id}", tier3(h.HandleRemoveTask))

	// History
	mux.HandleFunc("GET /history", contentNegotiated(h.HandleHistory, spa))

	// Stacks
	mux.HandleFunc(
		"GET /stacks",
		contentNegotiatedWithSSE(
			h.HandleListStacks,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventStack) },
			spa,
		),
	)
	mux.HandleFunc("GET /stacks/summary", contentNegotiated(h.HandleStackSummary, spa))
	mux.HandleFunc(
		"GET /stacks/{name}",
		contentNegotiatedWithSSE(h.HandleGetStack, func(w http.ResponseWriter, r *http.Request) {
			h.broadcaster.ServeSSE(w, r, sse.StackMatcher(h.cache, r.PathValue("name")))
		}, spa),
	)
	mux.Handle("DELETE /stacks/{name}", tier3(h.HandleRemoveStack))

	// Configs
	mux.HandleFunc(
		"GET /configs",
		contentNegotiatedWithSSE(
			h.HandleListConfigs,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventConfig) },
			spa,
		),
	)
	mux.HandleFunc(
		"GET /configs/{id}",
		contentNegotiatedWithSSE(
			h.HandleGetConfig,
			func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, cache.EventConfig, r.PathValue("id")) },
			spa,
		),
	)
	mux.Handle("DELETE /configs/{id}", tier3(h.HandleRemoveConfig))
	mux.Handle("POST /configs", tier2(h.HandleCreateConfig))
	mux.HandleFunc("GET /configs/{id}/labels", contentNegotiated(h.HandleGetConfigLabels, spa))
	mux.Handle("PATCH /configs/{id}/labels", tier2(h.HandlePatchConfigLabels))

	// Secrets
	mux.HandleFunc(
		"GET /secrets",
		contentNegotiatedWithSSE(
			h.HandleListSecrets,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventSecret) },
			spa,
		),
	)
	mux.HandleFunc(
		"GET /secrets/{id}",
		contentNegotiatedWithSSE(
			h.HandleGetSecret,
			func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, cache.EventSecret, r.PathValue("id")) },
			spa,
		),
	)
	mux.Handle("DELETE /secrets/{id}", tier3(h.HandleRemoveSecret))
	mux.Handle("POST /secrets", tier2(h.HandleCreateSecret))
	mux.HandleFunc("GET /secrets/{id}/labels", contentNegotiated(h.HandleGetSecretLabels, spa))
	mux.Handle("PATCH /secrets/{id}/labels", tier2(h.HandlePatchSecretLabels))

	// Networks
	mux.HandleFunc(
		"GET /networks",
		contentNegotiatedWithSSE(
			h.HandleListNetworks,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventNetwork) },
			spa,
		),
	)
	mux.HandleFunc(
		"GET /networks/{id}",
		contentNegotiatedWithSSE(
			h.HandleGetNetwork,
			func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, cache.EventNetwork, r.PathValue("id")) },
			spa,
		),
	)
	mux.Handle("DELETE /networks/{id}", tier3(h.HandleRemoveNetwork))

	// Volumes
	mux.HandleFunc(
		"GET /volumes",
		contentNegotiatedWithSSE(
			h.HandleListVolumes,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventVolume) },
			spa,
		),
	)
	mux.HandleFunc(
		"GET /volumes/{name}",
		contentNegotiatedWithSSE(
			h.HandleGetVolume,
			func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, cache.EventVolume, r.PathValue("name")) },
			spa,
		),
	)
	mux.Handle("DELETE /volumes/{name}", tier3(h.HandleRemoveVolume))

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
	handler = requireReady(h)(handler)
	handler = negotiate(handler)
	handler = auth.Middleware(authProvider)(handler)
	handler = securityHeaders(handler)
	handler = recovery(handler)
	handler = requestID(handler)
	return handler
}

func requireReady(h *Handlers) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !h.isReady() && isResourcePath(r.URL.Path) &&
				ContentTypeFromContext(r.Context()) == ContentTypeJSON {
				writeErrorCode(w, r, "ENG001", "Docker daemon is not reachable")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isResourcePath(path string) bool {
	switch {
	case strings.HasPrefix(path, "/-/"):
		return false
	case strings.HasPrefix(path, "/api"):
		return false
	case strings.HasPrefix(path, "/auth/"):
		return false
	case strings.HasPrefix(path, "/assets/"):
		return false
	case path == "/":
		return false
	default:
		return true
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().
			Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https:")
		next.ServeHTTP(w, r)
	})
}
