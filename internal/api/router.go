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
	"github.com/radiergummi/cetacean/internal/metrics"
)

// RouterConfig holds all dependencies and options for NewRouter.
type RouterConfig struct {
	Handlers          *Handlers
	Broadcaster       *sse.Broadcaster
	MetricsProxy      *prometheus.Proxy
	SPA               http.Handler
	OpenAPISpec       []byte
	ScalarJS          []byte
	EnablePprof       bool
	EnableSelfMetrics bool
	AuthProvider      auth.Provider
	BasePath          string
	CORS              *CORSConfig
	TLSEnabled        bool
}

func NewRouter(cfg RouterConfig) http.Handler {
	auth.SetErrorWriter(WriteErrorCode)

	h := cfg.Handlers
	b := cfg.Broadcaster
	metricsProxy := cfg.MetricsProxy
	spa := cfg.SPA
	authProvider := cfg.AuthProvider

	mux := http.NewServeMux()

	tier1 := requireLevel(config.OpsOperational, h.operationsLevel)
	tier2 := requireLevel(config.OpsConfiguration, h.operationsLevel)
	tier3 := requireLevel(config.OpsImpactful, h.operationsLevel)

	// ACL wrappers for write endpoints.
	svcACL := h.requireWriteACL(h.serviceName)
	nodeACL := h.requireWriteACL(h.nodeName)
	taskACL := h.requireWriteACL(h.taskServiceResource)
	stackACL := h.requireWriteACL(h.stackName)
	cfgACL := h.requireWriteACL(h.configName)
	secACL := h.requireWriteACL(h.secretName)
	netACL := h.requireWriteACL(h.networkName)
	volACL := h.requireWriteACL(h.volumeName)
	pluginACL := h.requireWriteACL(h.pluginName)
	pluginWildACL := h.requireWriteACL(wildcardResource("plugin"))
	cfgWildACL := h.requireWriteACL(wildcardResource("config"))
	secWildACL := h.requireWriteACL(wildcardResource("secret"))
	swarmACL := h.requireWriteACL(swarmResource)

	authProvider.RegisterRoutes(mux)
	mux.HandleFunc("GET /auth/whoami", auth.WhoamiHandler(authProvider))

	// Meta endpoints (no content negotiation, no discovery links)
	mux.HandleFunc("GET /-/health", h.HandleHealth)
	mux.HandleFunc("GET /-/ready", h.HandleReady)
	mux.HandleFunc("GET /-/docker-latest-version", h.HandleDockerLatestVersion)
	if cfg.EnableSelfMetrics {
		mux.Handle("GET /-/metrics", metrics.Handler())
	}
	// Metrics (content-negotiated: JSON → proxy, SSE → stream, HTML → SPA)
	mux.HandleFunc("GET /metrics/status", h.HandleMonitoringStatus)
	mux.HandleFunc("GET /metrics/labels", h.withAnyGrant(metricsProxy.HandleMetricsLabels))
	mux.HandleFunc(
		"GET /metrics/labels/{name}",
		h.withAnyGrant(metricsProxy.HandleMetricsLabelValues),
	)
	mux.HandleFunc("GET /metrics", contentNegotiatedWithSSE(
		h.withAnyGrant(metricsProxy.HandleMetrics),
		h.HandleMetricsStream,
		spa,
	))

	// API documentation (content-negotiated)
	mux.HandleFunc("GET /api", HandleAPIDoc(cfg.OpenAPISpec))
	mux.HandleFunc("GET /api/scalar.js", HandleScalarJS(cfg.ScalarJS))
	mux.HandleFunc("GET /api/context.jsonld", HandleContext)
	mux.HandleFunc("GET /api/errors", contentNegotiated(HandleErrorIndex, spa))
	mux.HandleFunc("GET /api/errors/{code}", contentNegotiated(HandleErrorDetail, spa))

	// SSE events
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		if ct != ContentTypeSSE {
			spa.ServeHTTP(w, r)
			return
		}
		b.ServeSSE(w, r, h.aclMatchWrap(r, nil), "")
	})

	// Cluster
	mux.HandleFunc("GET /cluster", contentNegotiated(h.HandleCluster, spa))
	mux.HandleFunc("GET /cluster/metrics", contentNegotiated(h.HandleClusterMetrics, spa))
	mux.HandleFunc("GET /cluster/capacity", contentNegotiated(h.HandleClusterCapacity, spa))
	mux.HandleFunc("GET /swarm", contentNegotiated(h.HandleSwarm, spa))
	mux.Handle("PATCH /swarm/orchestration", swarmACL(tier2(h.HandlePatchSwarmOrchestration)))
	mux.Handle("PATCH /swarm/raft", swarmACL(tier2(h.HandlePatchSwarmRaft)))
	mux.Handle("PATCH /swarm/dispatcher", swarmACL(tier2(h.HandlePatchSwarmDispatcher)))
	mux.Handle("PATCH /swarm/ca", swarmACL(tier3(h.HandlePatchSwarmCAConfig)))
	mux.Handle("PATCH /swarm/encryption", swarmACL(tier3(h.HandlePatchSwarmEncryption)))
	mux.Handle("POST /swarm/rotate-token", swarmACL(tier3(h.HandlePostRotateToken)))
	mux.Handle("POST /swarm/rotate-unlock-key", swarmACL(tier3(h.HandlePostRotateUnlockKey)))
	mux.Handle("POST /swarm/force-rotate-ca", swarmACL(tier3(h.HandlePostForceRotateCA)))
	mux.Handle("GET /swarm/unlock-key", swarmACL(tier3(h.HandleGetUnlockKey)))
	mux.Handle("POST /swarm/unlock", swarmACL(tier3(h.HandlePostUnlockSwarm)))
	mux.HandleFunc("GET /disk-usage", contentNegotiated(h.HandleDiskUsage, spa))
	// Plugins
	mux.HandleFunc("GET /plugins", contentNegotiated(h.HandleListPlugins, spa))
	mux.HandleFunc("GET /plugins/{name}", contentNegotiated(h.HandleGetPlugin, spa))
	mux.HandleFunc("GET /swarm/plugins", contentNegotiated(h.HandleListPlugins, spa))
	mux.Handle("POST /plugins/privileges", pluginWildACL(tier3(h.HandlePluginPrivileges)))
	mux.Handle("POST /plugins", pluginWildACL(tier3(h.HandleInstallPlugin)))
	mux.Handle("POST /plugins/{name}/enable", pluginACL(tier2(h.HandleEnablePlugin)))
	mux.Handle("POST /plugins/{name}/disable", pluginACL(tier2(h.HandleDisablePlugin)))
	mux.Handle("DELETE /plugins/{name}", pluginACL(tier3(h.HandleRemovePlugin)))
	mux.Handle("POST /plugins/{name}/upgrade", pluginACL(tier3(h.HandleUpgradePlugin)))
	mux.Handle("PATCH /plugins/{name}/settings", pluginACL(tier2(h.HandleConfigurePlugin)))

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
			func(w http.ResponseWriter, r *http.Request) {
				h.streamResource(w, r, cache.EventNode, r.PathValue("id"))
			},
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
			func(w http.ResponseWriter, r *http.Request) {
				h.streamResource(w, r, cache.EventService, r.PathValue("id"))
			},
			spa,
		),
	)
	mux.HandleFunc("GET /services/{id}/tasks", contentNegotiated(h.HandleServiceTasks, spa))
	mux.HandleFunc(
		"GET /services/{id}/logs",
		contentNegotiatedWithSSE(h.HandleServiceLogs, h.HandleServiceLogs, spa),
	)

	// Node write operations
	mux.Handle("PUT /nodes/{id}/availability", nodeACL(tier3(h.HandleUpdateNodeAvailability)))
	mux.HandleFunc("GET /nodes/{id}/labels", contentNegotiated(h.HandleGetNodeLabels, spa))
	mux.Handle("PATCH /nodes/{id}/labels", nodeACL(tier3(h.HandlePatchNodeLabels)))
	mux.HandleFunc("GET /nodes/{id}/role", contentNegotiated(h.HandleGetNodeRole, spa))
	mux.Handle("PUT /nodes/{id}/role", nodeACL(tier3(h.HandleUpdateNodeRole)))
	mux.Handle("DELETE /nodes/{id}", nodeACL(tier3(h.HandleRemoveNode)))

	// Service write operations — tier 1 (operational)
	mux.Handle("PUT /services/{id}/scale", svcACL(tier1(h.HandleScaleService)))
	mux.Handle("PUT /services/{id}/image", svcACL(tier1(h.HandleUpdateServiceImage)))
	mux.Handle("POST /services/{id}/rollback", svcACL(tier1(h.HandleRollbackService)))
	mux.Handle("POST /services/{id}/restart", svcACL(tier1(h.HandleRestartService)))

	// Service write operations — tier 2 (configuration)
	mux.HandleFunc("GET /services/{id}/env", contentNegotiated(h.HandleGetServiceEnv, spa))
	mux.Handle("PATCH /services/{id}/env", svcACL(tier2(h.HandlePatchServiceEnv)))
	mux.HandleFunc("GET /services/{id}/labels", contentNegotiated(h.HandleGetServiceLabels, spa))
	mux.Handle("PATCH /services/{id}/labels", svcACL(tier2(h.HandlePatchServiceLabels)))
	mux.HandleFunc(
		"GET /services/{id}/resources",
		contentNegotiated(h.HandleGetServiceResources, spa),
	)
	mux.Handle("PATCH /services/{id}/resources", svcACL(tier2(h.HandlePatchServiceResources)))
	mux.HandleFunc(
		"GET /services/{id}/healthcheck",
		contentNegotiated(h.HandleGetServiceHealthcheck, spa),
	)
	mux.Handle("PUT /services/{id}/healthcheck", svcACL(tier2(h.HandlePutServiceHealthcheck)))
	mux.Handle("PATCH /services/{id}/healthcheck", svcACL(tier2(h.HandlePatchServiceHealthcheck)))
	mux.HandleFunc(
		"GET /services/{id}/placement",
		contentNegotiated(h.HandleGetServicePlacement, spa),
	)
	mux.Handle("PUT /services/{id}/placement", svcACL(tier2(h.HandlePutServicePlacement)))
	mux.HandleFunc("GET /services/{id}/ports", contentNegotiated(h.HandleGetServicePorts, spa))
	mux.Handle("PATCH /services/{id}/ports", svcACL(tier2(h.HandlePatchServicePorts)))
	mux.HandleFunc(
		"GET /services/{id}/update-policy",
		contentNegotiated(h.HandleGetServiceUpdatePolicy, spa),
	)
	mux.Handle(
		"PATCH /services/{id}/update-policy",
		svcACL(tier2(h.HandlePatchServiceUpdatePolicy)),
	)
	mux.HandleFunc(
		"GET /services/{id}/rollback-policy",
		contentNegotiated(h.HandleGetServiceRollbackPolicy, spa),
	)
	mux.Handle(
		"PATCH /services/{id}/rollback-policy",
		svcACL(tier2(h.HandlePatchServiceRollbackPolicy)),
	)
	mux.HandleFunc(
		"GET /services/{id}/log-driver",
		contentNegotiated(h.HandleGetServiceLogDriver, spa),
	)
	mux.Handle("PATCH /services/{id}/log-driver", svcACL(tier2(h.HandlePatchServiceLogDriver)))
	mux.HandleFunc("GET /services/{id}/configs", contentNegotiated(h.HandleGetServiceConfigs, spa))
	mux.Handle("PATCH /services/{id}/configs", svcACL(tier2(h.HandlePatchServiceConfigs)))
	mux.HandleFunc("GET /services/{id}/secrets", contentNegotiated(h.HandleGetServiceSecrets, spa))
	mux.Handle("PATCH /services/{id}/secrets", svcACL(tier2(h.HandlePatchServiceSecrets)))
	mux.HandleFunc(
		"GET /services/{id}/networks",
		contentNegotiated(h.HandleGetServiceNetworks, spa),
	)
	mux.Handle("PATCH /services/{id}/networks", svcACL(tier2(h.HandlePatchServiceNetworks)))
	mux.HandleFunc("GET /services/{id}/mounts", contentNegotiated(h.HandleGetServiceMounts, spa))
	mux.Handle("PATCH /services/{id}/mounts", svcACL(tier2(h.HandlePatchServiceMounts)))

	mux.HandleFunc(
		"GET /services/{id}/container-config",
		contentNegotiated(h.HandleGetServiceContainerConfig, spa),
	)
	mux.Handle(
		"PATCH /services/{id}/container-config",
		svcACL(tier2(h.HandlePatchServiceContainerConfig)),
	)

	// Service write operations — tier 3 (impactful)
	mux.HandleFunc("GET /services/{id}/mode", contentNegotiated(h.HandleGetServiceMode, spa))
	mux.Handle("PUT /services/{id}/mode", svcACL(tier3(h.HandleUpdateServiceMode)))
	mux.HandleFunc(
		"GET /services/{id}/endpoint-mode",
		contentNegotiated(h.HandleGetServiceEndpointMode, spa),
	)
	mux.Handle("PUT /services/{id}/endpoint-mode", svcACL(tier3(h.HandleUpdateServiceEndpointMode)))
	mux.Handle("DELETE /services/{id}", svcACL(tier3(h.HandleRemoveService)))

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
			func(w http.ResponseWriter, r *http.Request) {
				h.streamResource(w, r, cache.EventTask, r.PathValue("id"))
			},
			spa,
		),
	)
	mux.HandleFunc(
		"GET /tasks/{id}/logs",
		contentNegotiatedWithSSE(h.HandleTaskLogs, h.HandleTaskLogs, spa),
	)
	mux.Handle("DELETE /tasks/{id}", taskACL(tier3(h.HandleRemoveTask)))

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
			stackMatch := sse.StackMatcher(h.cache, r.PathValue("name"))
			h.broadcaster.ServeSSE(w, r, h.aclMatchWrap(r, stackMatch), "")
		}, spa),
	)
	mux.Handle("DELETE /stacks/{name}", stackACL(tier3(h.HandleRemoveStack)))

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
			func(w http.ResponseWriter, r *http.Request) {
				h.streamResource(w, r, cache.EventConfig, r.PathValue("id"))
			},
			spa,
		),
	)
	mux.Handle("DELETE /configs/{id}", cfgACL(tier3(h.HandleRemoveConfig)))
	mux.Handle("POST /configs", cfgWildACL(tier2(h.HandleCreateConfig)))
	mux.HandleFunc("GET /configs/{id}/labels", contentNegotiated(h.HandleGetConfigLabels, spa))
	mux.Handle("PATCH /configs/{id}/labels", cfgACL(tier2(h.HandlePatchConfigLabels)))

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
			func(w http.ResponseWriter, r *http.Request) {
				h.streamResource(w, r, cache.EventSecret, r.PathValue("id"))
			},
			spa,
		),
	)
	mux.Handle("DELETE /secrets/{id}", secACL(tier3(h.HandleRemoveSecret)))
	mux.Handle("POST /secrets", secWildACL(tier2(h.HandleCreateSecret)))
	mux.HandleFunc("GET /secrets/{id}/labels", contentNegotiated(h.HandleGetSecretLabels, spa))
	mux.Handle("PATCH /secrets/{id}/labels", secACL(tier2(h.HandlePatchSecretLabels)))

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
			func(w http.ResponseWriter, r *http.Request) {
				h.streamResource(w, r, cache.EventNetwork, r.PathValue("id"))
			},
			spa,
		),
	)
	mux.Handle("DELETE /networks/{id}", netACL(tier3(h.HandleRemoveNetwork)))

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
			func(w http.ResponseWriter, r *http.Request) {
				h.streamResource(w, r, cache.EventVolume, r.PathValue("name"))
			},
			spa,
		),
	)
	mux.Handle("DELETE /volumes/{name}", volACL(tier3(h.HandleRemoveVolume)))

	// Search
	mux.HandleFunc("GET /search", contentNegotiated(h.HandleSearch, spa))

	// Profile
	mux.HandleFunc("GET /profile", contentNegotiated(h.HandleProfile, spa))

	// Topology
	mux.HandleFunc("GET /topology/networks", contentNegotiated(h.HandleNetworkTopology, spa))
	mux.HandleFunc("GET /topology/placement", contentNegotiated(h.HandlePlacementTopology, spa))

	// Profiling (opt-in via CETACEAN_PPROF=true)
	if cfg.EnablePprof {
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
	handler = cors(cfg.CORS)(handler)
	handler = securityHeaders(handler, cfg.TLSEnabled)
	handler = recovery(handler)
	handler = requestID(handler)
	return basePathMiddleware(cfg.BasePath, handler)
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

func securityHeaders(next http.Handler, tlsEnabled bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().
			Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https:")
		if tlsEnabled {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
