// Package mcp embeds a Model Context Protocol server in Cetacean. Resources
// expose cluster state and tools expose write operations to MCP-aware agents.
// It mounts behind the router's auth middleware; identity reaches mcp-go via
// WithHTTPContextFunc, and ACL is enforced per request inside each handler.
package mcp

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/mcp/oauth"
	"github.com/radiergummi/cetacean/internal/mcp/tracing"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// ProviderName identifies identities derived from MCP bearer tokens. Stamped
// on auth.Identity.Provider so downstream code can distinguish OAuth-vended
// MCP identities from regular Cetacean auth provider identities.
const ProviderName = "mcp-oauth"

// mcpInstructions and mcpDescription are the server-level usage contract sent
// in the initialize response. mcpTitle is what a host listing several servers
// shows, falling back to the programmatic name; mcpWebsiteURL is the "what is
// this" link beside it.
const (
	mcpInstructions = "Cetacean is a read-mostly observability and operations interface for a Docker Swarm cluster. Resolve a resource's ID or name with the find tool before reading its details or applying a write. Reads (the cetacean:// resources and the get_logs/find tools) are subject to per-resource ACL like everything else; mutating tools are additionally gated by an operations tier, and either kind may be hidden from tools/list or rejected at call time. Prefer the cetacean:// resources for detail and cross-references; use tools to change cluster state. Tool results include structuredContent you can parse directly. Named investigation and remediation sequences over these resources and tools are available via prompts/list."

	mcpDescription = "Read and safely operate a Docker Swarm cluster."

	mcpTitle = "Cetacean — Docker Swarm"

	//nolint:gosec // G101: the project's public documentation site, not a credential — gosec flags the URL-shaped constant name.
	mcpWebsiteURL = "https://cetacean.mazetti.me"
)

// DockerWriteClient is the narrow surface of Docker write operations the MCP
// tools invoke; docker.Client and api.DockerWriteClient both satisfy it. It
// composes one writer per operations tier, so a fake can implement only the
// surface it exercises.
type DockerWriteClient interface {
	ServiceLifecycleWriter
	ServiceSpecWriter
	NodeWriter
	ResourceRemover
	ResourceCreator
}

// RecommendationEngine is the narrow surface of the recommendations engine
// consumed by the MCP server. *recommendations.Engine satisfies it; passing
// nil (CETACEAN_RECOMMENDATIONS=false) is also acceptable.
type RecommendationEngine interface {
	Results() []recommendations.Recommendation
}

// Server is the Cetacean MCP server.
type Server struct {
	cache          *cache.Cache
	writeClient    DockerWriteClient
	logs           LogStreamer
	acl            *acl.Evaluator
	config         config.MCPConfig
	globalOpsLevel config.OperationsLevel
	oauth          *oauth.Server // nil when auth mode is "none"
	authMode       string        // upstream auth mode ("cert", "oidc", ...); used for bypass match
	authProvider   auth.Provider // upstream auth provider; used when bypass is active
	mcpServer      *mcpserver.MCPServer
	httpServer     *mcpserver.StreamableHTTPServer
	recEngine      RecommendationEngine
	prom           MetricsQuerier // nil when Prometheus is not configured

	// allowedOrigins is the set of Origin header values the streamable HTTP
	// endpoint accepts. originGuard rejects any other non-empty Origin with
	// 403 (DNS-rebinding defense). allowAnyOrigin disables the check.
	allowedOrigins []string
	allowAnyOrigin bool

	// iconBaseURL is the canonical external base (issuer + base path) that tool
	// icon `src` values are built from, e.g. "https://cetacean.example.com".
	// Icons live under the un-authed /assets/ prefix. Empty disables icons.
	iconBaseURL string

	// registeredTools is the subset of toolCatalog() the server actually wired
	// into mcp-go after applying the operations-level tier filter. The
	// per-identity tools/list filter (filterToolsForIdentity) inspects this
	// slice to know which tools may need further hiding from a given caller.
	registeredTools []toolDef

	// registeredPrompts holds the prompts that passed the tier gate, so the
	// per-identity prompts/list filter (filterPromptsForIdentity) can read
	// each one's driven tools back.
	registeredPrompts []promptDef

	// notifications tracks per-session resource subscriptions and fans cache
	// events out as MCP notifications. cancelNotifications is the listener
	// detach returned by cache.AddOnChangeListener.
	notifications       *NotificationManager
	cancelNotifications func()

	// watches bounds concurrent `watch` waits — see maxConcurrentWatches. A nil
	// channel disables the bound, which is what a Server built without New gets;
	// only New wires it, and only New serves real traffic.
	watches   chan struct{}
	closeOnce sync.Once
}

// Options bundles the dependencies for New. A nil OAuth mounts /mcp without
// bearer validation, which is only safe in auth mode "none"; Recommendations
// is nil when the engine is off. When AuthMode appears in Config.AuthBypass,
// identity comes from AuthProvider instead of an OAuth bearer.
type Options struct {
	WriteClient     DockerWriteClient
	Logs            LogStreamer
	ACL             *acl.Evaluator
	Config          config.MCPConfig
	GlobalOpsLevel  config.OperationsLevel
	OAuth           *oauth.Server
	AuthMode        string
	AuthProvider    auth.Provider
	Recommendations RecommendationEngine
	AllowedOrigins  []string

	// AllowAnyOrigin disables the Origin check. It is the caller's reading of
	// server.cors.origins — api.CORSConfig.Wildcard() — rather than a "*" this
	// package looks for itself, so one setting cannot mean a wildcard here and
	// a literal list to cross-origin protection.
	AllowAnyOrigin bool

	// Prometheus backs the get_metrics tool. Nil when CETACEAN_PROMETHEUS_URL
	// is unset, and the tool then says metrics are unavailable rather than
	// charting nothing.
	Prometheus MetricsQuerier

	// IconBaseURL is the canonical external base URL (issuer + base path) used
	// to build absolute tool-icon `src` values. Icons are served from the
	// embedded frontend under /assets/mcp-icons/. Empty disables tool icons.
	IconBaseURL string

	// Tracer records spans for dispatched MCP methods and tool calls. Nil
	// leaves mcp-go's noop tracer in place, which is the zero-config path:
	// tracing costs nothing until CETACEAN_OTEL_ENDPOINT is set.
	Tracer oteltrace.Tracer
}

// Cache TTLs advertised on cacheable results: freshness hints, not guarantees.
// Both are short because an agent acting on a minute-old service list can
// scale the wrong thing. listChanged remains the primary invalidation signal;
// these only bound how long a client that missed one stays stale.
const (
	cacheTTLList = 30 * time.Second
	cacheTTLRead = 10 * time.Second
)

// New constructs an MCP server, registering its resources and tools once.
// The returned *Server exposes Handler() for mounting on an http.ServeMux.
func New(c *cache.Cache, opts Options) (*Server, error) {
	if c == nil {
		return nil, errors.New("mcp: cache is required")
	}

	srv := &Server{
		cache:          c,
		writeClient:    opts.WriteClient,
		logs:           opts.Logs,
		acl:            opts.ACL,
		config:         opts.Config,
		globalOpsLevel: opts.GlobalOpsLevel,
		oauth:          opts.OAuth,
		authMode:       opts.AuthMode,
		authProvider:   opts.AuthProvider,
		recEngine:      opts.Recommendations,
		prom:           opts.Prometheus,
		allowedOrigins: opts.AllowedOrigins,
		allowAnyOrigin: opts.AllowAnyOrigin,
		iconBaseURL:    strings.TrimRight(opts.IconBaseURL, "/"),
		notifications:  NewNotificationManager(),
		watches:        make(chan struct{}, maxConcurrentWatches),
	}

	serverOptions := []mcpserver.ServerOption{
		mcpserver.WithResourceCapabilities(true, true),
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithToolFilter(srv.filterToolsForIdentity),
		// Redundant — AddPrompt enables the capability implicitly, as AddTool
		// and AddResource do — but declared beside the other two so the block
		// does not read as though prompts were special. listChanged is false:
		// the catalog cannot change while the process runs.
		mcpserver.WithPromptCapabilities(false),
		mcpserver.WithPromptFilter(srv.filterPromptsForIdentity),
		// Declared beside the two providers on purpose: mcp-go's defaults
		// answer completion/complete with an empty list, so advertising the
		// capability alone leaves a client with a permanently blank dropdown.
		mcpserver.WithCompletions(),
		mcpserver.WithResourceCompletionProvider(srv),
		mcpserver.WithPromptCompletionProvider(srv),
		// Load-bearing, not defensive: a task-augmented call runs from
		// executeRegularToolAsTask on a bare goroutine, past api's recovery
		// middleware and with no recover of its own. That goroutine does apply
		// the tool middleware chain, which is what this installs.
		mcpserver.WithRecovery(),
		mcpserver.WithResourceRecovery(),
		mcpserver.WithHooks(srv.installSubscriptionHooks()),
		mcpserver.WithInstructions(mcpInstructions),
		mcpserver.WithDescription(mcpDescription),
		mcpserver.WithTitle(mcpTitle),
		mcpserver.WithWebsiteURL(mcpWebsiteURL),
		// Nil when no icon base URL is configured, exactly as a tool's icons
		// are: an icon `src` must be an absolute https:// or data: URI per the
		// spec, so without a canonical external base there is no icon to name.
		mcpserver.WithIcons(srv.icon("server", "cetacean")...),
		mcpserver.WithExtensions(serverExtensions()),
		// A tool result whose structuredContent does not conform to its
		// declared outputSchema is rejected.
		mcpserver.WithOutputSchemaValidation(),
		// SEP-2549 freshness hints. The scope is always private: every
		// response is filtered by the caller's grants, so an intermediary
		// caching one identity's view and serving it to another would be an
		// authorization bypass.
		mcpserver.WithCacheHints(cacheTTLList.Milliseconds(), mcplib.CacheScopePrivate),
		mcpserver.WithMethodCacheHints(
			mcplib.MethodResourcesRead,
			cacheTTLRead.Milliseconds(),
			mcplib.CacheScopePrivate,
		),
		// Tasks (2026-07-28): poll-based, since the revision removed
		// tasks/list. Must stay paired with serverExtensions' extensionTasks
		// entry — a host told of an extension it cannot call is worse off than
		// one told nothing.
		mcpserver.WithTaskCapabilities(
			false, /* list */
			true,  /* cancel */
			true,  /* toolCallTasks */
		),
		// Caps tasks running at once, NOT tasks retained: mcp-go releases the
		// counter when a task finishes but keeps its record. Retention is
		// bounded separately, by installTaskTTLHook — mcp-go offers no server
		// option for it.
		mcpserver.WithMaxConcurrentTasks(opts.Config.MaxConcurrentTasks),
	}

	// SEP-414 trace context. Installed only when a tracer is configured: the
	// options are cheap but WithTracer also appends a tool middleware, and an
	// untraced deployment should carry none of it.
	if opts.Tracer != nil {
		serverOptions = append(serverOptions,
			mcpserver.WithTracer(tracing.NewTracer(opts.Tracer)),
			// _meta is the transport-agnostic path the 2026-07-28 convention
			// specifies, and the one agent hosts use.
			mcpserver.WithMetaPropagator(tracing.NewMetaPropagator()),
			// Headers cover the host that traces at the HTTP layer instead,
			// so a call joins the trace of the request that delivered it.
			mcpserver.WithPropagator(tracing.NewPropagator()),
		)
	}

	mcpSrv := mcpserver.NewMCPServer(
		"cetacean",
		"1.0.0",
		serverOptions...,
	)
	srv.mcpServer = mcpSrv

	srv.registerResources()
	srv.registerUIResources()
	srv.registerTools()
	srv.registerPrompts()
	srv.cancelNotifications = srv.startNotifications()

	httpSrv := mcpserver.NewStreamableHTTPServer(mcpSrv,
		// Protocol 2026-07-28 has no sessions, and requireModernProtocol
		// turns away everything older, so there is no session state to keep:
		// each request is served by its own ephemeral session.
		mcpserver.WithStateful(false),
		mcpserver.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			// The bearer-validating middleware (see Handler) stamps an
			// auth.Identity on r.Context() before mcp-go sees the request.
			// Bridge it into the MCP-handler context so resources/tools can
			// look it up via auth.IdentityFromContext.
			if id := auth.IdentityFromContext(r.Context()); id != nil {
				ctx = auth.ContextWithIdentity(ctx, id)
			}

			// server/discover reports this as supportedVersions. mcp-go would
			// otherwise list every revision it implements, advertising eras
			// requireModernProtocol turns away and sending a well-behaved
			// client straight into a rejection.
			return mcpserver.WithSupportedProtocolVersions(
				ctx,
				[]string{mcplib.LATEST_PROTOCOL_VERSION},
			)
		}),
	)
	srv.httpServer = httpSrv

	return srv, nil
}

// Close releases external resources held by the server. Currently this means
// detaching the cache change listener so the cache can be torn down without
// firing into a stale notification path. Safe to call multiple times and
// safe under concurrent callers thanks to sync.Once.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		if s.cancelNotifications != nil {
			s.cancelNotifications()
			s.cancelNotifications = nil
		}
	})
}

// Handler returns the http.Handler for the MCP endpoint. With OAuth
// configured it validates the bearer token; without, it serves the raw mcp-go
// handler unguarded, which is only safe in auth mode "none".
func (s *Server) Handler() http.Handler {
	// The protocol gate sits innermost so that origin and bearer checks answer
	// first: an unauthenticated caller learns nothing about what we speak.
	h := s.requireModernProtocol(s.httpServer)
	if s.oauth != nil {
		h = s.bearerAuth(h)
	}

	return s.originGuard(h)
}

// originGuard is the DNS-rebinding defense the Streamable HTTP transport
// requires and mcp-go does not enforce. A request with no Origin passes
// through. Matching is exact: what counts as a wildcard stays internal/api's
// ruling, and arrives here as AllowAnyOrigin.
func (s *Server) originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if s.allowAnyOrigin || origin == "" || slices.Contains(s.allowedOrigins, origin) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "forbidden origin", http.StatusForbidden)
	})
}

// bearerAuth validates Authorization: Bearer <jwt>, puts an auth.Identity in
// the request context, and answers 401 with RFC 6750 WWW-Authenticate
// otherwise. When Config.AuthBypass names the active upstream mode, that
// provider runs first and its identity skips JWT verification.
func (s *Server) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.bypassActive() {
			if id, err := s.authProvider.Authenticate(
				newDiscardingResponseWriter(),
				r,
			); err == nil &&
				id != nil {
				ctx := auth.ContextWithIdentity(r.Context(), id)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		token := auth.ExtractBearerToken(r)
		if token == "" {
			s.oauth.WriteUnauthorized(w, "invalid_token")
			return
		}

		claims, err := s.oauth.VerifyAccessToken(token)
		if err != nil {
			s.oauth.WriteUnauthorized(w, "invalid_token")
			return
		}

		identity := &auth.Identity{
			Subject:  claims.Subject,
			Groups:   claims.Groups,
			Provider: ProviderName,
		}
		ctx := auth.ContextWithIdentity(r.Context(), identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bypassActive reports whether the upstream auth mode is in the configured
// CETACEAN_MCP_AUTH_BYPASS list and a provider is available to consult.
func (s *Server) bypassActive() bool {
	if s.authProvider == nil || s.authMode == "" {
		return false
	}
	return slices.Contains(s.config.AuthBypass, s.authMode)
}

// discardingResponseWriter swallows writes from a provider invoked only for
// its identity, so AuthBypass must name providers that write nothing on the
// success path. Header() is persistent, so a provider setting a header and
// reading it back sees itself. The zero value is unusable.
type discardingResponseWriter struct{ h http.Header }

func newDiscardingResponseWriter() discardingResponseWriter {
	return discardingResponseWriter{h: make(http.Header)}
}

func (d discardingResponseWriter) Header() http.Header       { return d.h }
func (discardingResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (discardingResponseWriter) WriteHeader(int)             {}

// toolVisibility describes which tools an identity may see and which resource
// types it can read. Both are nil when everything is visible, mirroring
// acl.Evaluator.Can. An identity matching no grant needs no special case:
// acl.TypeAccess answers false for every type.
type toolVisibility struct {
	allow    func(name string) bool
	readable func(resourceType string) bool
}

// toolVisibilityFor turns an identity's grants into a per-tool and a per-type
// read predicate. The grant-flattening lives in acl.TypeGrants, beside the
// call-time rule it shadows; only the mapping from tools to the keys they
// check is stated here.
func (s *Server) toolVisibilityFor(ctx context.Context) toolVisibility {
	if s.acl == nil {
		return toolVisibility{}
	}

	identity := auth.IdentityFromContext(ctx)
	if identity == nil {
		return toolVisibility{}
	}

	// One policy read answers both questions. Asking separately would let a
	// hot reload land in between and answer each from a different policy.
	access := s.acl.TypeGrants(identity)
	if access.AllowAll() {
		return toolVisibility{}
	}

	return toolVisibility{
		allow: func(name string) bool {
			spec, gated := toolACLSpecs[name]
			if !gated {
				return true
			}
			if spec.resourceType == "*" {
				// Tool acts across all types — visible if any grant exists.
				return access.HasAnyGrant()
			}

			return access.Can(spec.permission, spec.resourceType)
		},
		readable: func(resourceType string) bool {
			return access.Can("read", resourceType)
		},
	}
}

// filterToolsForIdentity hides tools whose resource type the identity has no
// grant on, so tools/list reflects what the caller can invoke. Tier gating
// already happened at registration. The filter is advisory: call-time ACL
// still enforces, and the goal here is only a truthful list.
func (s *Server) filterToolsForIdentity(ctx context.Context, tools []mcplib.Tool) []mcplib.Tool {
	visibility := s.toolVisibilityFor(ctx)
	if visibility.allow == nil {
		return tools
	}

	return filterToolsByACLSpec(tools, visibility.allow)
}

// filterPromptsForIdentity gates prompts/list, and prompts/get through the
// same "not found", so a hidden prompt cannot be confirmed by guessing.
// Visibility is all-or-nothing: a sequence the caller cannot finish dead-ends
// partway, for a remediation prompt possibly after the model has written.
func (s *Server) filterPromptsForIdentity(
	ctx context.Context,
	prompts []mcplib.Prompt,
) []mcplib.Prompt {
	visibility := s.toolVisibilityFor(ctx)
	if visibility.allow == nil {
		return prompts
	}

	defs := make(map[string]promptDef, len(s.registeredPrompts))
	for _, def := range s.registeredPrompts {
		defs[def.prompt.Name] = def
	}

	out := make([]mcplib.Prompt, 0, len(prompts))
	for _, prompt := range prompts {
		// Fail closed: a prompt we cannot resolve back to its declaration, or
		// one declaring no tools or no read types, has nothing to check
		// visibility against — and both checks are vacuously true for an empty
		// set.
		def, known := defs[prompt.Name]
		if !known || len(def.drives) == 0 || len(def.reads) == 0 {
			continue
		}

		if allToolsVisible(def.drives, visibility.allow) &&
			allTypesReadable(def.reads, visibility.readable) {
			out = append(out, prompt)
		}
	}

	return out
}

// allToolsVisible reports whether every named tool passes allow.
func allToolsVisible(tools []string, allow func(name string) bool) bool {
	for _, name := range tools {
		if !allow(name) {
			return false
		}
	}

	return true
}

// allTypesReadable reports whether the identity holds a read grant on every
// resource type the prompt walks. This is what the driven-tool check cannot
// answer: a sequence built from ungated cross-type tools is callable by anyone
// while returning nothing but empty ACL-filtered lists.
func allTypesReadable(types []string, readable func(resourceType string) bool) bool {
	for _, resourceType := range types {
		if !readable(resourceType) {
			return false
		}
	}

	return true
}

// toolACLSpec describes the resource type and permission required to call a
// tool. Used by filterToolsForIdentity to decide whether to surface the tool
// in tools/list. Tools not present in toolACLSpecs are always visible.
type toolACLSpec struct {
	resourceType string
	permission   string
}

// toolACLSpecs maps tool names to the ACL key they evaluate at call time.
// Keep aligned with the handlers in tools.go — adding a tool without an
// entry here defaults to "always visible", which is safe (call-time ACL still
// enforces) but means tools/list may advertise tools the caller cannot use.
var toolACLSpecs = map[string]toolACLSpec{
	"get_logs":               {"service", "read"},
	"watch":                  {"service", "read"},
	"scale_service":          {"service", "write"},
	"update_service_image":   {"service", "write"},
	"rollback_service":       {"service", "write"},
	"restart_service":        {"service", "write"},
	"remove_task":            {"service", "write"}, // task ACL delegates to parent service
	"update_service":         {"service", "write"},
	"update_node_labels":     {"node", "write"},
	"update_node":            {"node", "write"},
	"remove_service":         {"service", "write"},
	"update_service_secrets": {"service", "write"},
	"update_service_configs": {"service", "write"},
	"update_service_mounts":  {"service", "write"},
	"create_config":          {"config", "write"},
	"create_secret":          {"secret", "write"},
	"remove_config":          {"config", "write"},
	"remove_secret":          {"secret", "write"},
	"remove_network":         {"network", "write"},
	"remove_volume":          {"volume", "write"},
	// "find" is intentionally absent — it returns hits across many types,
	// each individually ACL-filtered, so it should remain visible even to
	// callers with grants on only a subset.
}

func filterToolsByACLSpec(tools []mcplib.Tool, allow func(name string) bool) []mcplib.Tool {
	out := make([]mcplib.Tool, 0, len(tools))
	for _, t := range tools {
		if allow(t.Name) {
			out = append(out, t)
		}
	}
	return out
}
