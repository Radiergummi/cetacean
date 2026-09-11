package api

import (
	"context"
	"net/http"
	"net/http/pprof"
	"net/netip"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/metrics"
	"github.com/radiergummi/cetacean/internal/prometheus"
)

// Resyncer triggers a manual full re-fetch of cluster state. Backed by the
// watcher's Resync method. Decoupled as an interface so the api package
// stays free of docker-package imports.
type Resyncer interface {
	Resync(ctx context.Context) error
}

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
	PublicURL         string
	CORS              *CORSConfig
	TLSEnabled        bool

	// InlineScriptHashes are CSP `'sha256-…'` tokens for the SPA's inline
	// scripts, from InlineScriptHashes. Empty means no inline script runs.
	InlineScriptHashes []string

	TrustedProxies []netip.Prefix
	Resyncer       Resyncer

	// MCPHandler, when non-nil, is mounted at {BasePath}/mcp. main.go builds
	// it from internal/mcp; the api package stays decoupled from mcp-go.
	MCPHandler http.Handler

	// OAuthRoutes, when non-nil, registers the OAuth 2.1 authorization server
	// endpoints (/.well-known/*, /oauth/*) on the mux. Wired by main.go from
	// internal/mcp/oauth.
	OAuthRoutes func(mux *http.ServeMux, basePath string)
}

// listFeeds builds feedHandlers for a resource list endpoint. Every one of
// them renders its rows as CSV, so the flag is set here rather than at the
// eight call sites.
func (h *Handlers) listFeeds(title string, eventType cache.EventType) feedHandlers {
	return feedHandlers{
		atom:     h.feedListHandler(title, eventType, renderAtom),
		jsonFeed: h.feedListHandler(title, eventType, renderJSONFeed),
		csv:      true,
	}
}

// searchFeeds builds feedHandlers for the search endpoint.
func (h *Handlers) searchFeeds() feedHandlers {
	return feedHandlers{
		atom:        h.feedSearchHandler(renderAtom),
		jsonFeed:    h.feedSearchHandler(renderJSONFeed),
		queryParams: searchFeedParams,
	}
}

// detailFeeds builds feedHandlers for a resource detail endpoint.
func (h *Handlers) detailFeeds(
	eventType cache.EventType,
	idParam string,
	nameFunc func(id string) string,
) feedHandlers {
	return feedHandlers{
		atom:     h.feedDetailHandler(eventType, idParam, nameFunc, renderAtom),
		jsonFeed: h.feedDetailHandler(eventType, idParam, nameFunc, renderJSONFeed),
	}
}

// routeRecorder is the mux NewRouter registers on: an http.ServeMux that also
// remembers the patterns it was handed. The stdlib mux exposes no way to
// enumerate them, and without the list nothing can hold the routes that exist
// against the ones api/openapi.yaml documents — a walk that starts from the
// spec cannot see a route the spec never mentions.
//
// Routes another component registers directly on the wrapped mux — the auth
// provider's, the OAuth server's — are not recorded. Both sit under paths the
// spec does not describe.
type routeRecorder struct {
	mux      *http.ServeMux
	patterns []string
}

func (r *routeRecorder) Handle(pattern string, handler http.Handler) {
	r.patterns = append(r.patterns, pattern)
	r.mux.Handle(pattern, handler)
}

func (r *routeRecorder) HandleFunc(
	pattern string,
	handler func(http.ResponseWriter, *http.Request),
) {
	r.patterns = append(r.patterns, pattern)
	r.mux.HandleFunc(pattern, handler)
}

func (r *routeRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func NewRouter(cfg RouterConfig) http.Handler {
	handler, _ := newRouter(cfg)

	return handler
}

// newRouter assembles the router and returns the patterns it registered beside
// it. Production calls NewRouter and drops the second value; the spec-parity
// test reads it.
func newRouter(cfg RouterConfig) (http.Handler, []string) {
	auth.SetErrorWriter(WriteErrorCode)

	h := cfg.Handlers
	b := cfg.Broadcaster
	metricsProxy := cfg.MetricsProxy
	spa := cfg.SPA
	authProvider := cfg.AuthProvider

	mux := &routeRecorder{mux: http.NewServeMux()}

	tier1 := requireLevel(config.OpsOperational, h.operationsLevel)
	tier2 := requireLevel(config.OpsConfiguration, h.operationsLevel)
	tier3 := requireLevel(config.OpsImpactful, h.operationsLevel)

	// ACL wrappers for write endpoints.
	svcACL := h.requireWriteACL(
		resolveResource(
			"service",
			h.cache.GetService,
			func(s swarm.Service) string { return s.Spec.Name },
		),
	)
	nodeACL := h.requireWriteACL(resolveResource("node", h.cache.GetNode, nodeHostnameOrID))
	taskACL := h.requireWriteACL(h.taskServiceResource)
	stackACL := h.requireWriteACL(pathResource("stack", "name"))
	cfgACL := h.requireWriteACL(
		resolveResource(
			"config",
			h.cache.GetConfig,
			func(c swarm.Config) string { return c.Spec.Name },
		),
	)
	secACL := h.requireWriteACL(
		resolveResource(
			"secret",
			h.cache.GetSecret,
			func(s swarm.Secret) string { return s.Spec.Name },
		),
	)
	netACL := h.requireWriteACL(
		resolveResource(
			"network",
			h.cache.GetNetwork,
			func(n network.Summary) string { return n.Name },
		),
	)
	volACL := h.requireWriteACL(pathResource("volume", "name"))
	pluginACL := h.requireWriteACL(pathResource("plugin", "name"))
	pluginWildACL := h.requireWriteACL(wildcardResource("plugin"))
	cfgWildACL := h.requireWriteACL(wildcardResource("config"))
	secWildACL := h.requireWriteACL(wildcardResource("secret"))
	swarmACL := h.requireWriteACL(swarmResource)

	// Derived (ACL, tier) chains for the gated route registrations below.
	svcTier1 := NewChain(svcACL, tier1)
	svcTier2 := NewChain(svcACL, tier2)
	svcTier3 := NewChain(svcACL, tier3)
	nodeTier2 := NewChain(nodeACL, tier2)
	nodeTier3 := NewChain(nodeACL, tier3)
	taskTier3 := NewChain(taskACL, tier3)
	stackTier3 := NewChain(stackACL, tier3)
	cfgTier2 := NewChain(cfgACL, tier2)
	cfgTier3 := NewChain(cfgACL, tier3)
	secTier2 := NewChain(secACL, tier2)
	secTier3 := NewChain(secACL, tier3)
	netTier3 := NewChain(netACL, tier3)
	volTier3 := NewChain(volACL, tier3)
	pluginTier2 := NewChain(pluginACL, tier2)
	pluginTier3 := NewChain(pluginACL, tier3)
	pluginWildTier3 := NewChain(pluginWildACL, tier3)
	cfgWildTier2 := NewChain(cfgWildACL, tier2)
	secWildTier2 := NewChain(secWildACL, tier2)
	swarmTier2 := NewChain(swarmACL, tier2)
	swarmTier3 := NewChain(swarmACL, tier3)

	authProvider.RegisterRoutes(mux.mux)
	mux.HandleFunc("GET /auth/whoami", auth.WhoamiHandler(authProvider, writeIdentityJSONLD))

	// Meta endpoints (no content negotiation, no discovery links)
	mux.HandleFunc("GET /-/health", h.HandleHealth)
	mux.HandleFunc("GET /-/ready", h.HandleReady)
	mux.HandleFunc("GET /-/docker-latest-version", h.HandleDockerLatestVersion)
	mux.HandleFunc("GET /-/licenses", HandleLicenses)
	mux.HandleFunc("GET /-/licenses/texts/{id}", HandleLicenseText)
	mux.HandleFunc("GET /-/notices", HandleNotices)
	// Canonical URL is /-/sbom.cdx.json; the negotiate middleware strips the
	// .json suffix before dispatch, so the mux route omits it.
	mux.HandleFunc("GET /-/sbom.cdx", HandleSBOM)
	if cfg.EnableSelfMetrics {
		mux.Handle("GET /-/metrics", metrics.Handler())
	}
	if cfg.Resyncer != nil {
		mux.Handle("POST /-/resync", HandleResync(cfg.Resyncer))
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
		feedHandlers{},
		spa,
	))

	// API documentation (content-negotiated)
	mux.HandleFunc("GET /api", HandleAPIDoc(cfg.OpenAPISpec))
	mux.HandleFunc("GET /api/scalar.js", HandleScalarJS(cfg.ScalarJS))
	mux.HandleFunc("GET /api/context.jsonld", HandleContext)
	mux.HandleFunc("GET "+openSearchPath, HandleOpenSearch)
	mux.HandleFunc("GET "+apiCatalogPath, HandleAPICatalog(catalogMounts{
		mcp:           cfg.MCPHandler != nil,
		oauthMetadata: cfg.OAuthRoutes != nil,
	}))
	mux.HandleFunc("GET /api/errors", contentNegotiated(HandleErrorIndex, feedHandlers{}, spa))
	mux.HandleFunc(
		"GET /api/errors/{code}",
		contentNegotiated(HandleErrorDetail, feedHandlers{}, spa),
	)

	// SSE events
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		switch ct {
		case ContentTypeSSE:
			b.ServeSSE(w, r, h.aclMatchWrap(r, nil), "")
		case ContentTypeAtom:
			h.handleFeedHistory(w, r, renderAtom)
		case ContentTypeJSONFeed:
			h.handleFeedHistory(w, r, renderJSONFeed)
		case ContentTypeHTML:
			spa.ServeHTTP(w, r)
		default:
			notAcceptable(
				w, r,
				"text/event-stream, text/html, application/atom+xml, application/feed+json",
			)
		}
	})

	// Cluster
	mux.HandleFunc("GET /cluster", contentNegotiated(h.HandleCluster, feedHandlers{}, spa))
	mux.HandleFunc(
		"GET /cluster/metrics",
		contentNegotiated(h.HandleClusterMetrics, feedHandlers{}, spa),
	)
	mux.HandleFunc(
		"GET /cluster/capacity",
		contentNegotiated(h.HandleClusterCapacity, feedHandlers{}, spa),
	)
	mux.HandleFunc("GET /swarm", contentNegotiated(h.HandleSwarm, feedHandlers{}, spa))
	mux.Handle("PATCH /swarm/orchestration", swarmTier2.ThenFunc(h.HandlePatchSwarmOrchestration))
	mux.Handle("PATCH /swarm/raft", swarmTier2.ThenFunc(h.HandlePatchSwarmRaft))
	mux.Handle("PATCH /swarm/dispatcher", swarmTier2.ThenFunc(h.HandlePatchSwarmDispatcher))
	mux.Handle("PATCH /swarm/ca", swarmTier3.ThenFunc(h.HandlePatchSwarmCAConfig))
	mux.Handle("PATCH /swarm/encryption", swarmTier3.ThenFunc(h.HandlePatchSwarmEncryption))
	mux.Handle("POST /swarm/rotate-token", swarmTier3.ThenFunc(h.HandlePostRotateToken))
	mux.Handle("POST /swarm/rotate-unlock-key", swarmTier3.ThenFunc(h.HandlePostRotateUnlockKey))
	mux.Handle("POST /swarm/force-rotate-ca", swarmTier3.ThenFunc(h.HandlePostForceRotateCA))
	mux.Handle("GET /swarm/unlock-key", swarmTier3.ThenFunc(h.HandleGetUnlockKey))
	mux.Handle("POST /swarm/unlock", swarmTier3.ThenFunc(h.HandlePostUnlockSwarm))
	mux.HandleFunc("GET /disk-usage", contentNegotiated(h.HandleDiskUsage, feedHandlers{}, spa))
	// Plugins
	mux.HandleFunc("GET /plugins", contentNegotiated(h.HandleListPlugins, feedHandlers{}, spa))
	mux.HandleFunc("GET /plugins/{name}", contentNegotiated(h.HandleGetPlugin, feedHandlers{}, spa))
	mux.HandleFunc(
		"GET /swarm/plugins",
		contentNegotiated(h.HandleListPlugins, feedHandlers{}, spa),
	)
	mux.Handle("POST /plugins/privileges", pluginWildTier3.ThenFunc(h.HandlePluginPrivileges))
	mux.Handle("POST /plugins", pluginWildTier3.ThenFunc(h.HandleInstallPlugin))
	mux.Handle("POST /plugins/{name}/enable", pluginTier2.ThenFunc(h.HandleEnablePlugin))
	mux.Handle("POST /plugins/{name}/disable", pluginTier2.ThenFunc(h.HandleDisablePlugin))
	mux.Handle("DELETE /plugins/{name}",
		pluginTier3.Append(h.precond(h.pluginRepresentation)).
			ThenFunc(h.HandleRemovePlugin))
	mux.Handle("POST /plugins/{name}/upgrade", pluginTier3.ThenFunc(h.HandleUpgradePlugin))
	mux.Handle("PATCH /plugins/{name}/settings", pluginTier2.ThenFunc(h.HandleConfigurePlugin))

	// Nodes
	mux.HandleFunc(
		"GET /nodes",
		contentNegotiatedWithSSE(
			h.HandleListNodes,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventNode) },
			h.listFeeds("Nodes", cache.EventNode),
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
			h.detailFeeds(cache.EventNode, "id", func(id string) string {
				if n, ok := h.cache.GetNode(id); ok {
					return n.Description.Hostname
				}
				return id
			}),
			spa,
		),
	)
	mux.HandleFunc(
		"GET /nodes/{id}/tasks",
		contentNegotiated(h.HandleNodeTasks, feedHandlers{csv: true}, spa),
	)

	// Recommendations
	mux.HandleFunc(
		"GET /recommendations",
		contentNegotiated(h.HandleRecommendations, feedHandlers{
			atom:     h.feedRecommendationsHandler(renderAtom),
			jsonFeed: h.feedRecommendationsHandler(renderJSONFeed),
			csv:      true,
		}, spa),
	)

	// Services
	mux.HandleFunc(
		"GET /services",
		contentNegotiatedWithSSE(
			h.HandleListServices,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventService) },
			h.listFeeds("Services", cache.EventService),
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
			h.detailFeeds(cache.EventService, "id", func(id string) string {
				if s, ok := h.cache.GetService(id); ok {
					return s.Spec.Name
				}
				return id
			}),
			spa,
		),
	)
	mux.HandleFunc(
		"GET /services/{id}/tasks",
		contentNegotiated(h.HandleServiceTasks, feedHandlers{csv: true}, spa),
	)
	mux.HandleFunc(
		"GET /services/{id}/logs",
		contentNegotiatedWithSSE(h.HandleServiceLogs, h.HandleServiceLogs, feedHandlers{}, spa),
	)

	// Node write operations
	mux.Handle("PUT /nodes/{id}/availability", nodeTier3.ThenFunc(h.HandleUpdateNodeAvailability))
	mux.HandleFunc(
		"GET /nodes/{id}/labels",
		contentNegotiated(h.HandleGetNodeLabels, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /nodes/{id}/labels",
		nodeTier2.Append(h.precond(h.nodeLabelsSpec().representation)).
			ThenFunc(h.HandlePatchNodeLabels))
	mux.HandleFunc(
		"GET /nodes/{id}/role",
		contentNegotiated(h.HandleGetNodeRole, feedHandlers{}, spa),
	)
	mux.Handle("PUT /nodes/{id}/role",
		nodeTier3.Append(h.precond(h.nodeRoleRepresentation)).
			ThenFunc(h.HandleUpdateNodeRole))
	mux.Handle("DELETE /nodes/{id}",
		nodeTier3.Append(h.precond(h.nodeRepresentation)).
			ThenFunc(h.HandleRemoveNode))

	// Service write operations — tier 1 (operational)
	mux.Handle("PUT /services/{id}/scale", svcTier1.ThenFunc(h.HandleScaleService))
	mux.Handle("PUT /services/{id}/image", svcTier1.ThenFunc(h.HandleUpdateServiceImage))
	mux.Handle("POST /services/{id}/rollback", svcTier1.ThenFunc(h.HandleRollbackService))
	mux.Handle("POST /services/{id}/restart", svcTier1.ThenFunc(h.HandleRestartService))

	// Service write operations — tier 2 (configuration)
	mux.HandleFunc(
		"GET /services/{id}/env",
		contentNegotiated(h.HandleGetServiceEnv, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /services/{id}/env",
		svcTier2.Append(h.precond(h.serviceEnvRepresentation)).
			ThenFunc(h.HandlePatchServiceEnv))
	mux.HandleFunc(
		"GET /services/{id}/labels",
		contentNegotiated(h.HandleGetServiceLabels, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /services/{id}/labels",
		svcTier2.Append(h.precond(h.serviceLabelsSpec().representation)).
			ThenFunc(h.HandlePatchServiceLabels))
	mux.HandleFunc(
		"GET /services/{id}/resources",
		contentNegotiated(h.HandleGetServiceResources, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /services/{id}/resources",
		svcTier2.Append(h.precond(h.serviceResourcesRepresentation)).
			ThenFunc(h.HandlePatchServiceResources))
	mux.HandleFunc(
		"GET /services/{id}/healthcheck",
		contentNegotiated(h.HandleGetServiceHealthcheck, feedHandlers{}, spa),
	)
	// One representation, two methods: PUT replaces the healthcheck and PATCH
	// merges into it, but both are conditioned on the same current state.
	svcHealthcheckTier2 := svcTier2.Append(h.precond(h.serviceHealthcheckRepresentation))
	mux.Handle(
		"PUT /services/{id}/healthcheck",
		svcHealthcheckTier2.ThenFunc(h.HandlePutServiceHealthcheck),
	)
	mux.Handle(
		"PATCH /services/{id}/healthcheck",
		svcHealthcheckTier2.ThenFunc(h.HandlePatchServiceHealthcheck),
	)
	mux.HandleFunc(
		"GET /services/{id}/placement",
		contentNegotiated(h.HandleGetServicePlacement, feedHandlers{}, spa),
	)
	mux.Handle("PUT /services/{id}/placement",
		svcTier2.Append(h.precond(h.servicePlacementRepresentation)).
			ThenFunc(h.HandlePutServicePlacement))
	mux.HandleFunc(
		"GET /services/{id}/ports",
		contentNegotiated(h.HandleGetServicePorts, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /services/{id}/ports",
		svcTier2.Append(h.precond(h.servicePortsRepresentation)).
			ThenFunc(h.HandlePatchServicePorts))
	mux.HandleFunc(
		"GET /services/{id}/update-policy",
		contentNegotiated(h.HandleGetServiceUpdatePolicy, feedHandlers{}, spa),
	)
	mux.Handle(
		"PATCH /services/{id}/update-policy",
		svcTier2.Append(h.precond(h.serviceUpdatePolicyRepresentation)).
			ThenFunc(h.HandlePatchServiceUpdatePolicy),
	)
	mux.HandleFunc(
		"GET /services/{id}/rollback-policy",
		contentNegotiated(h.HandleGetServiceRollbackPolicy, feedHandlers{}, spa),
	)
	mux.Handle(
		"PATCH /services/{id}/rollback-policy",
		svcTier2.Append(h.precond(h.serviceRollbackPolicyRepresentation)).
			ThenFunc(h.HandlePatchServiceRollbackPolicy),
	)
	mux.HandleFunc(
		"GET /services/{id}/log-driver",
		contentNegotiated(h.HandleGetServiceLogDriver, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /services/{id}/log-driver",
		svcTier2.Append(h.precond(h.serviceLogDriverRepresentation)).
			ThenFunc(h.HandlePatchServiceLogDriver))
	mux.HandleFunc(
		"GET /services/{id}/configs",
		contentNegotiated(h.HandleGetServiceConfigs, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /services/{id}/configs",
		svcTier2.Append(h.precond(h.serviceConfigsRepresentation)).
			ThenFunc(h.HandlePatchServiceConfigs))
	mux.HandleFunc(
		"GET /services/{id}/secrets",
		contentNegotiated(h.HandleGetServiceSecrets, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /services/{id}/secrets",
		svcTier2.Append(h.precond(h.serviceSecretsRepresentation)).
			ThenFunc(h.HandlePatchServiceSecrets))
	mux.HandleFunc(
		"GET /services/{id}/networks",
		contentNegotiated(h.HandleGetServiceNetworks, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /services/{id}/networks",
		svcTier2.Append(h.precond(h.serviceNetworksRepresentation)).
			ThenFunc(h.HandlePatchServiceNetworks))
	mux.HandleFunc(
		"GET /services/{id}/mounts",
		contentNegotiated(h.HandleGetServiceMounts, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /services/{id}/mounts",
		svcTier2.Append(h.precond(h.serviceMountsRepresentation)).
			ThenFunc(h.HandlePatchServiceMounts))

	mux.HandleFunc(
		"GET /services/{id}/container-config",
		contentNegotiated(h.HandleGetServiceContainerConfig, feedHandlers{}, spa),
	)
	mux.Handle(
		"PATCH /services/{id}/container-config",
		svcTier2.Append(h.precond(h.serviceContainerConfigRepresentation)).
			ThenFunc(h.HandlePatchServiceContainerConfig),
	)

	// Service write operations — tier 3 (impactful)
	mux.HandleFunc(
		"GET /services/{id}/mode",
		contentNegotiated(h.HandleGetServiceMode, feedHandlers{}, spa),
	)
	mux.Handle("PUT /services/{id}/mode",
		svcTier3.Append(h.precond(h.serviceModeRepresentation)).
			ThenFunc(h.HandleUpdateServiceMode))
	mux.HandleFunc(
		"GET /services/{id}/endpoint-mode",
		contentNegotiated(h.HandleGetServiceEndpointMode, feedHandlers{}, spa),
	)
	mux.Handle(
		"PUT /services/{id}/endpoint-mode",
		svcTier3.Append(h.precond(h.serviceEndpointModeRepresentation)).
			ThenFunc(h.HandleUpdateServiceEndpointMode),
	)
	mux.Handle("DELETE /services/{id}",
		svcTier3.Append(h.precond(h.serviceRepresentation)).
			ThenFunc(h.HandleRemoveService))

	// Tasks
	mux.HandleFunc(
		"GET /tasks",
		contentNegotiatedWithSSE(
			h.HandleListTasks,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventTask) },
			h.listFeeds("Tasks", cache.EventTask),
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
			h.detailFeeds(cache.EventTask, "id", func(id string) string {
				return id
			}),
			spa,
		),
	)
	mux.HandleFunc(
		"GET /tasks/{id}/logs",
		contentNegotiatedWithSSE(h.HandleTaskLogs, h.HandleTaskLogs, feedHandlers{}, spa),
	)
	mux.Handle("DELETE /tasks/{id}",
		taskTier3.Append(h.precond(h.taskRepresentation)).
			ThenFunc(h.HandleRemoveTask))

	// History
	mux.HandleFunc("GET /history", contentNegotiated(h.HandleHistory, feedHandlers{
		atom:     h.feedHistoryHandler(renderAtom),
		jsonFeed: h.feedHistoryHandler(renderJSONFeed),
		csv:      true,
	}, spa))

	// Stacks
	mux.HandleFunc(
		"GET /stacks",
		contentNegotiatedWithSSE(
			h.HandleListStacks,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventStack) },
			h.listFeeds("Stacks", cache.EventStack),
			spa,
		),
	)
	mux.HandleFunc(
		"GET /stacks/summary",
		contentNegotiated(h.HandleStackSummary, feedHandlers{}, spa),
	)
	mux.HandleFunc(
		"GET /stacks/{name}",
		contentNegotiatedWithSSE(h.HandleGetStack, func(w http.ResponseWriter, r *http.Request) {
			stackMatch := sse.StackMatcher(h.cache, r.PathValue("name"))
			h.broadcaster.ServeSSE(w, r, h.aclMatchWrap(r, stackMatch), "")
		}, h.detailFeeds(cache.EventStack, "name", func(name string) string {
			return name
		}), spa),
	)
	mux.Handle("DELETE /stacks/{name}",
		stackTier3.Append(h.precond(h.stackRepresentation)).
			ThenFunc(h.HandleRemoveStack))

	// Configs
	mux.HandleFunc(
		"GET /configs",
		contentNegotiatedWithSSE(
			h.HandleListConfigs,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventConfig) },
			h.listFeeds("Configs", cache.EventConfig),
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
			h.detailFeeds(cache.EventConfig, "id", func(id string) string {
				if c, ok := h.cache.GetConfig(id); ok {
					return c.Spec.Name
				}
				return id
			}),
			spa,
		),
	)
	mux.Handle("DELETE /configs/{id}",
		cfgTier3.Append(h.precond(h.configRepresentation)).
			ThenFunc(h.HandleRemoveConfig))
	mux.Handle("POST /configs", cfgWildTier2.ThenFunc(h.HandleCreateConfig))
	mux.HandleFunc(
		"GET /configs/{id}/labels",
		contentNegotiated(h.HandleGetConfigLabels, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /configs/{id}/labels",
		cfgTier2.Append(h.precond(h.configLabelsSpec().representation)).
			ThenFunc(h.HandlePatchConfigLabels))

	// Secrets
	mux.HandleFunc(
		"GET /secrets",
		contentNegotiatedWithSSE(
			h.HandleListSecrets,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventSecret) },
			h.listFeeds("Secrets", cache.EventSecret),
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
			h.detailFeeds(cache.EventSecret, "id", func(id string) string {
				if s, ok := h.cache.GetSecret(id); ok {
					return s.Spec.Name
				}
				return id
			}),
			spa,
		),
	)
	mux.Handle("DELETE /secrets/{id}",
		secTier3.Append(h.precond(h.secretRepresentation)).
			ThenFunc(h.HandleRemoveSecret))
	mux.Handle("POST /secrets", secWildTier2.ThenFunc(h.HandleCreateSecret))
	mux.HandleFunc(
		"GET /secrets/{id}/labels",
		contentNegotiated(h.HandleGetSecretLabels, feedHandlers{}, spa),
	)
	mux.Handle("PATCH /secrets/{id}/labels",
		secTier2.Append(h.precond(h.secretLabelsSpec().representation)).
			ThenFunc(h.HandlePatchSecretLabels))

	// Networks
	mux.HandleFunc(
		"GET /networks",
		contentNegotiatedWithSSE(
			h.HandleListNetworks,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventNetwork) },
			h.listFeeds("Networks", cache.EventNetwork),
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
			h.detailFeeds(cache.EventNetwork, "id", func(id string) string {
				if n, ok := h.cache.GetNetwork(id); ok {
					return n.Name
				}
				return id
			}),
			spa,
		),
	)
	mux.Handle("DELETE /networks/{id}",
		netTier3.Append(h.precond(h.networkRepresentation)).
			ThenFunc(h.HandleRemoveNetwork))

	// Volumes
	mux.HandleFunc(
		"GET /volumes",
		contentNegotiatedWithSSE(
			h.HandleListVolumes,
			func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventVolume) },
			h.listFeeds("Volumes", cache.EventVolume),
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
			h.detailFeeds(cache.EventVolume, "name", func(name string) string {
				return name
			}),
			spa,
		),
	)
	mux.Handle("DELETE /volumes/{name}",
		volTier3.Append(h.precond(h.volumeRepresentation)).
			ThenFunc(h.HandleRemoveVolume))

	// Search
	mux.HandleFunc("GET /search", contentNegotiated(h.HandleSearch, h.searchFeeds(), spa))

	// Profile
	mux.HandleFunc("GET /profile", contentNegotiated(h.HandleProfile, feedHandlers{}, spa))

	// Topology
	mux.HandleFunc("GET /topology", func(w http.ResponseWriter, r *http.Request) {
		switch ContentTypeFromContext(r.Context()) {
		case ContentTypeHTML:
			spa.ServeHTTP(w, r)
		case ContentTypeJGF, ContentTypeJSON:
			h.HandleTopology(w, r)
		case ContentTypeGraphML:
			h.HandleTopologyGraphML(w, r)
		case ContentTypeDOT:
			h.HandleTopologyDOT(w, r)
		default:
			notAcceptable(
				w, r,
				"application/vnd.jgf+json, application/graphml+xml, text/vnd.graphviz",
			)
		}
	})

	// The two projections removed in 0.13.0 need explicit routes, or the SPA
	// catch-all on "/" answers them: neither path carries a mid-path extension,
	// so a JSON client of the old endpoint gets 200 and a rendered dashboard
	// instead of an error — a worse failure than the deprecation it replaced,
	// because nothing about it looks like one. 410 rather than 404 says the
	// path is gone for good rather than merely absent today.
	for _, removed := range []string{"/topology/networks", "/topology/placement"} {
		mux.HandleFunc("GET "+removed, func(w http.ResponseWriter, r *http.Request) {
			writeErrorCode(
				w,
				r,
				"API012",
				"this projection was removed; GET /topology serves the whole graph, "+
					"and both views are derived from it",
			)
		})
	}

	// Profiling (opt-in via CETACEAN_PPROF=true)
	if cfg.EnablePprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	// MCP server (mounted before SPA fallback so /mcp doesn't fall through).
	if cfg.MCPHandler != nil {
		mux.Handle("/mcp", cfg.MCPHandler)
	}

	// OAuth 2.1 authorization server endpoints. Wired by main.go when MCP is
	// enabled and an auth provider is configured; the api package itself
	// doesn't reach into mcp/oauth.
	if cfg.OAuthRoutes != nil {
		cfg.OAuthRoutes(mux.mux, "")
	}

	// SPA fallback (must be last). It refuses only a type nothing serves,
	// rather than everything but text/html: */* resolves to JSON, so on this
	// route JSON means "unknown" rather than "a client asked for JSON" — and
	// every static file the dashboard pulls (/assets/*, the icons,
	// manifest.webmanifest) arrives that way.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if ContentTypeFromContext(r.Context()) == ContentTypeUnsupported {
			notAcceptable(w, r, "text/html")
			return
		}

		spa.ServeHTTP(w, r)
	})

	stack := NewChain(
		requestID,
		realIP(cfg.TrustedProxies),
		recovery,
		securityHeaders(cfg.TLSEnabled, cfg.InlineScriptHashes),
		cors(cfg.CORS),
		crossOriginProtection(cfg.CORS, cfg.PublicURL),
		auth.Middleware(authProvider),
		negotiate,
		requireReady(h),
		discoveryLinks,
		requestLogger,
	)

	return publicURLMiddleware(
		cfg.PublicURL,
		basePathMiddleware(cfg.BasePath, stack.Then(mux)),
	), mux.patterns
}

func requireReady(h *Handlers) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ct := ContentTypeFromContext(r.Context())
			if !h.isReady() && isResourcePath(r.URL.Path) &&
				(ct == ContentTypeJSON || ct == ContentTypeAtom || ct == ContentTypeJSONFeed ||
					ct == ContentTypeJGF || ct == ContentTypeGraphML || ct == ContentTypeDOT) {
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

func securityHeaders(tlsEnabled bool, inlineScriptHashes []string) Constructor {
	// Built once: the policy is the same on every response, and hashing the
	// SPA's inline scripts per request would be pure waste.
	csp := contentSecurityPolicy(inlineScriptHashes)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", csp)
			if tlsEnabled {
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeIdentityJSONLD writes an auth identity as a JSON-LD DetailResponse.
// Used by the /auth/whoami handler.
func writeIdentityJSONLD(w http.ResponseWriter, r *http.Request, id *auth.Identity) {
	writeJSON(w, NewDetailResponse(r.Context(), "/auth/whoami", "Identity", id))
}
