// Package mcp embeds a Model Context Protocol server in Cetacean. Resources
// expose the cluster state (services, nodes, tasks, ...) and tools expose
// write operations (scale, update, restart, ...) to MCP-aware agents.
//
// The server is mounted on Cetacean's HTTP router behind the existing auth
// middleware. Identity flows from HTTP context to mcp-go via
// WithHTTPContextFunc; ACL is enforced per request inside resource and tool
// handlers.
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
// to clients in the initialize response (WithInstructions/WithDescription).
// Kept as package constants so the contract has a single source of truth
// shared with the test that asserts it surfaces.
const (
	mcpInstructions = "Cetacean is a read-mostly observability and operations interface for a Docker Swarm cluster. Resolve a resource's ID or name with the search tool before reading its details or applying a write. Reads (the cetacean:// resources and the get_logs/search tools) are always available; mutating tools are gated by an operations tier and per-resource ACL, and may be hidden from tools/list or rejected at call time. Prefer the cetacean:// resources for detail and cross-references; use tools to change cluster state. Tool results include structuredContent you can parse directly."

	mcpDescription = "Read and safely operate a Docker Swarm cluster."
)

// DockerWriteClient is the narrow surface of Docker write operations the MCP
// tools invoke. The concrete docker.Client and the existing api.DockerWriteClient
// both satisfy it via Go's structural typing.
//
// Tools are split across three operations tiers (see Design § Tools). The
// interface composes one writer per tier so test fakes can implement only the
// surface they exercise.
type DockerWriteClient interface {
	ServiceLifecycleWriter
	ServiceSpecWriter
	NodeWriter
	ResourceRemover
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

	// allowedOrigins is the set of Origin header values the streamable HTTP
	// endpoint accepts. originGuard rejects any other non-empty Origin with
	// 403 (DNS-rebinding defense). "*" disables the check.
	allowedOrigins []string

	// iconBaseURL is the canonical external base (issuer + base path) that tool
	// icon `src` values are built from, e.g. "https://cetacean.example.com".
	// Icons live under the un-authed /assets/ prefix. Empty disables icons.
	iconBaseURL string

	// registeredTools is the subset of toolCatalog() the server actually wired
	// into mcp-go after applying the operations-level tier filter. The
	// per-identity tools/list filter (filterToolsForIdentity) inspects this
	// slice to know which tools may need further hiding from a given caller.
	registeredTools []toolDef

	// notifications tracks per-session resource subscriptions and fans cache
	// events out as MCP notifications. cancelNotifications is the listener
	// detach returned by cache.AddOnChangeListener.
	notifications       *NotificationManager
	cancelNotifications func()
	closeOnce           sync.Once
}

// Options bundles the dependencies for New. OAuth is optional — when nil, the
// MCP endpoint is mounted without bearer-token validation (matching auth mode
// "none"). Recommendations may be nil when CETACEAN_RECOMMENDATIONS=false.
// AuthMode and AuthProvider together drive the CETACEAN_MCP_AUTH_BYPASS path:
// when AuthMode appears in Config.AuthBypass, the MCP server derives identity
// from AuthProvider (e.g. the mTLS cert) instead of requiring an OAuth bearer.
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

	// IconBaseURL is the canonical external base URL (issuer + base path) used
	// to build absolute tool-icon `src` values. Icons are served from the
	// embedded frontend under /assets/mcp-icons/. Empty disables tool icons.
	IconBaseURL string

	// Tracer records spans for dispatched MCP methods and tool calls. Nil
	// leaves mcp-go's noop tracer in place, which is the zero-config path:
	// tracing costs nothing until CETACEAN_OTEL_ENDPOINT is set.
	Tracer oteltrace.Tracer
}

// New constructs an MCP server. The returned *Server exposes Handler() for
// mounting on an http.ServeMux. registerResources and registerTools are
// invoked once during construction; tools and resources are then served from
// the mcp-go shared registry.
// Cache TTLs advertised on cacheable results. They are freshness hints, not
// correctness guarantees: a client may serve a cached response for this long
// before re-fetching. Both are deliberately short — Cetacean mirrors live
// cluster state, and an agent acting on a minute-old service list can scale the
// wrong thing. listChanged notifications remain the primary invalidation
// signal; these hints only bound how long a client that missed one stays stale.
const (
	cacheTTLList = 30 * time.Second
	cacheTTLRead = 10 * time.Second
)

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
		allowedOrigins: opts.AllowedOrigins,
		iconBaseURL:    strings.TrimRight(opts.IconBaseURL, "/"),
		notifications:  NewNotificationManager(),
	}

	serverOptions := []mcpserver.ServerOption{
		mcpserver.WithResourceCapabilities(true, true),
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithToolFilter(srv.filterToolsForIdentity),
		mcpserver.WithHooks(srv.installSubscriptionHooks()),
		mcpserver.WithInstructions(mcpInstructions),
		mcpserver.WithDescription(mcpDescription),
		mcpserver.WithExtensions(serverExtensions()),
		// Enforce advertised output schemas: a tool result whose
		// structuredContent does not conform to its declared outputSchema is
		// rejected. Only the curated-shape tools (search, get_logs, remove_*)
		// declare a schema; Docker-passthrough mutations carry none and skip
		// validation.
		mcpserver.WithOutputSchemaValidation(),
		// SEP-2549 freshness hints, applied by mcp-go to tools/list,
		// resources/list, resources/templates/list, resources/read and
		// server/discover, and omitted for clients on protocol versions older
		// than 2026-07-28, where the fields do not exist.
		//
		// The scope is always private: every Cetacean response is filtered by
		// the caller's ACL grants, so a shared intermediary caching one
		// identity's view and serving it to another would be an authorization
		// bypass.
		mcpserver.WithCacheHints(cacheTTLList.Milliseconds(), mcplib.CacheScopePrivate),
		mcpserver.WithMethodCacheHints(
			mcplib.MethodResourcesRead,
			cacheTTLRead.Milliseconds(),
			mcplib.CacheScopePrivate,
		),
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
	srv.registerTools()
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

// Handler returns the http.Handler for the MCP endpoint. When OAuth is
// configured, the handler validates the bearer token and emits
// `WWW-Authenticate` with the protected-resource metadata URL on failure;
// otherwise it serves the raw mcp-go handler unguarded (only safe with auth
// mode "none").
func (s *Server) Handler() http.Handler {
	// The protocol gate sits innermost so that origin and bearer checks answer
	// first: an unauthenticated caller learns nothing about what we speak.
	h := s.requireModernProtocol(s.httpServer)
	if s.oauth != nil {
		h = s.bearerAuth(h)
	}

	return s.originGuard(h)
}

// originGuard rejects requests bearing a forged Origin header with 403, a
// DNS-rebinding defense the MCP Streamable HTTP transport requires (mcp-go
// does not enforce it). Requests with no Origin (non-browser agents) pass
// through unaffected, as does any Origin when AllowedOrigins contains "*".
// Kept self-contained (exact match + wildcard) to avoid importing internal/api.
func (s *Server) originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		for _, allowed := range s.allowedOrigins {
			if allowed == "*" || allowed == origin {
				next.ServeHTTP(w, r)
				return
			}
		}
		http.Error(w, "forbidden origin", http.StatusForbidden)
	})
}

// bearerAuth validates Authorization: Bearer <jwt> on each request, populates
// an auth.Identity in the request context, and chains to next. On missing or
// invalid tokens it emits a 401 with RFC 6750 WWW-Authenticate carrying the
// protected-resource metadata URL.
//
// When CETACEAN_MCP_AUTH_BYPASS names the active upstream auth mode (typically
// "cert"), the upstream provider's Authenticate runs first; on success its
// identity is used and JWT verification is skipped. This lets mTLS-authenticated
// clients reach /mcp without taking the OAuth detour.
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

// discardingResponseWriter swallows writes from upstream providers invoked for
// identity-only bypass checks. Providers that would normally write a redirect
// or partial body (OIDC) end up with their output dropped — callers should
// only list providers that don't write on the success path (cert, headers,
// tailscale) in AuthBypass.
//
// Header() returns a persistent http.Header so a provider that sets a header
// then reads it back (e.g. setting WWW-Authenticate and checking the value
// before WriteHeader) sees consistent state. The zero value is unusable; call
// newDiscardingResponseWriter.
type discardingResponseWriter struct{ h http.Header }

func newDiscardingResponseWriter() discardingResponseWriter {
	return discardingResponseWriter{h: make(http.Header)}
}

func (d discardingResponseWriter) Header() http.Header       { return d.h }
func (discardingResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (discardingResponseWriter) WriteHeader(int)             {}

// filterToolsForIdentity is wired as a WithToolFilter for tools/list. Tier
// gating already happened at registration; this filter additionally hides
// tools whose primary resource type the identity has zero grants on, so the
// catalog reflects what the caller can actually invoke. The filter is
// advisory — call-time ACL still enforces, so returning the full slice would
// never grant anything; the goal here is a truthful list.
func (s *Server) filterToolsForIdentity(ctx context.Context, tools []mcplib.Tool) []mcplib.Tool {
	if s.acl == nil {
		return tools
	}
	identity := auth.IdentityFromContext(ctx)
	if identity == nil {
		return tools
	}
	perms := s.acl.PermissionsFor(identity)
	if perms == nil {
		// nil policy = allow all (mirrors acl.Evaluator.Can).
		return tools
	}
	if len(perms) == 0 {
		// Identity has zero grants. Drop everything except tools that don't
		// require any ACL check (none today, but kept defensive).
		return filterToolsByACLSpec(tools, func(name string) bool {
			_, gated := toolACLSpecs[name]
			return !gated
		})
	}
	allowedByType := make(map[string]map[string]bool, 2) // permission → type → bool
	for pattern, perms := range perms {
		resType, _, ok := splitResourceType(pattern)
		if !ok {
			continue
		}
		for _, perm := range perms {
			byType, ok := allowedByType[perm]
			if !ok {
				byType = make(map[string]bool)
				allowedByType[perm] = byType
			}
			byType[resType] = true
		}
	}
	// "write" implies "read" — mirror Evaluator.hasPermission.
	if writers, ok := allowedByType["write"]; ok {
		readers := allowedByType["read"]
		if readers == nil {
			readers = make(map[string]bool, len(writers))
			allowedByType["read"] = readers
		}
		for t := range writers {
			readers[t] = true
		}
	}
	return filterToolsByACLSpec(tools, func(name string) bool {
		spec, gated := toolACLSpecs[name]
		if !gated {
			return true
		}
		if spec.resourceType == "*" {
			// Tool acts across all types — visible if any grant exists.
			return true
		}
		byType := allowedByType[spec.permission]
		if byType == nil {
			return false
		}
		return byType[spec.resourceType] || byType["*"]
	})
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
	"get_logs":                       {"service", "read"},
	"scale_service":                  {"service", "write"},
	"update_service_image":           {"service", "write"},
	"rollback_service":               {"service", "write"},
	"restart_service":                {"service", "write"},
	"remove_task":                    {"service", "write"}, // task ACL delegates to parent service
	"update_service_env":             {"service", "write"},
	"update_service_labels":          {"service", "write"},
	"update_node_labels":             {"node", "write"},
	"update_service_resources":       {"service", "write"},
	"update_service_placement":       {"service", "write"},
	"update_service_ports":           {"service", "write"},
	"update_service_update_policy":   {"service", "write"},
	"update_service_rollback_policy": {"service", "write"},
	"update_service_log_driver":      {"service", "write"},
	"update_node_availability":       {"node", "write"},
	"update_node_role":               {"node", "write"},
	"remove_service":                 {"service", "write"},
	"remove_config":                  {"config", "write"},
	"remove_secret":                  {"secret", "write"},
	"remove_network":                 {"network", "write"},
	"remove_volume":                  {"volume", "write"},
	// "search" is intentionally absent — it returns hits across many types,
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

// splitResourceType pulls the type prefix from a grant resource pattern such
// as "service:web-*" → ("service", "web-*", true). Patterns like "*" or
// "*:*" yield ("*", ..., true) so callers can treat them as wildcards.
func splitResourceType(pattern string) (string, string, bool) {
	if pattern == "*" {
		return "*", "*", true
	}
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == ':' {
			return pattern[:i], pattern[i+1:], true
		}
	}
	return "", "", false
}
