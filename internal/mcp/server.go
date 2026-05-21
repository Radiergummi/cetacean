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
	"sync"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/mcp/oauth"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// ProviderName identifies identities derived from MCP bearer tokens. Stamped
// on auth.Identity.Provider so downstream code can distinguish OAuth-vended
// MCP identities from regular Cetacean auth provider identities.
const ProviderName = "mcp-oauth"

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
}

// New constructs an MCP server. The returned *Server exposes Handler() for
// mounting on an http.ServeMux. registerResources and registerTools are
// invoked once during construction; tools and resources are then served from
// the mcp-go shared registry.
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
		notifications:  NewNotificationManager(),
	}

	mcpSrv := mcpserver.NewMCPServer(
		"cetacean",
		"1.0.0",
		mcpserver.WithResourceCapabilities(true, true),
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithToolFilter(srv.filterToolsForIdentity),
		mcpserver.WithHooks(srv.installSubscriptionHooks()),
	)
	srv.mcpServer = mcpSrv

	srv.registerResources()
	srv.registerTools()
	srv.cancelNotifications = srv.startNotifications()

	httpSrv := mcpserver.NewStreamableHTTPServer(mcpSrv,
		mcpserver.WithStateful(true),
		mcpserver.WithSessionIdleTTL(opts.Config.SessionIdleTTL),
		mcpserver.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			// The bearer-validating middleware (see Handler) stamps an
			// auth.Identity on r.Context() before mcp-go sees the request.
			// Bridge it into the MCP-handler context so resources/tools can
			// look it up via auth.IdentityFromContext.
			if id := auth.IdentityFromContext(r.Context()); id != nil {
				ctx = auth.ContextWithIdentity(ctx, id)
			}
			return ctx
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
	if s.oauth == nil {
		return s.httpServer
	}
	return s.bearerAuth(s.httpServer)
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
			if id, err := s.authProvider.Authenticate(discardingResponseWriter{}, r); err == nil && id != nil {
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
type discardingResponseWriter struct{}

func (discardingResponseWriter) Header() http.Header         { return http.Header{} }
func (discardingResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (discardingResponseWriter) WriteHeader(int)             {}

// filterToolsForIdentity is wired as a WithToolFilter for tools/list. Tier
// gating already happened at registration, and per-resource ACL is enforced
// inside each handler at call time, so this is a pass-through. Refinements
// (e.g. hiding write tools from identities with no write grants at all) can
// add filtering here without widening access — returning the full slice
// cannot grant anything the call-time ACL check would otherwise deny.
func (s *Server) filterToolsForIdentity(_ context.Context, tools []mcplib.Tool) []mcplib.Tool {
	return tools
}
