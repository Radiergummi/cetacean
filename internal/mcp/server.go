// Package mcp embeds a Model Context Protocol server in Cetacean. Resources
// expose the cluster state (services, nodes, tasks, ...) and tools expose
// write operations (scale, update, restart, ...) to MCP-aware agents.
//
// The server is mounted on Cetacean's HTTP router behind the existing auth
// middleware. Identity flows from HTTP context to mcp-go via
// WithHTTPContextFunc; ACL is enforced per request inside resource and tool
// handlers. Real resource and tool registration arrives in later tasks of the
// implementation plan; this file is the wiring.
package mcp

import (
	"context"
	"errors"
	"net/http"

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
// tools need. The concrete docker.Client and the existing api.DockerWriteClient
// both satisfy it; real method shape lands with Task 10 when tools register.
type DockerWriteClient any

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
	acl            *acl.Evaluator
	config         config.MCPConfig
	globalOpsLevel config.OperationsLevel
	oauth          *oauth.Server // nil when auth mode is "none"
	mcpServer      *mcpserver.MCPServer
	httpServer     *mcpserver.StreamableHTTPServer
	recEngine      RecommendationEngine
}

// Options bundles the dependencies for New. OAuth is optional — when nil, the
// MCP endpoint is mounted without bearer-token validation (matching auth mode
// "none"). Recommendations may be nil when CETACEAN_RECOMMENDATIONS=false.
type Options struct {
	WriteClient     DockerWriteClient
	ACL             *acl.Evaluator
	Config          config.MCPConfig
	GlobalOpsLevel  config.OperationsLevel
	OAuth           *oauth.Server
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
		acl:            opts.ACL,
		config:         opts.Config,
		globalOpsLevel: opts.GlobalOpsLevel,
		oauth:          opts.OAuth,
		recEngine:      opts.Recommendations,
	}

	mcpSrv := mcpserver.NewMCPServer(
		"cetacean",
		"1.0.0",
		mcpserver.WithResourceCapabilities(true, true),
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithToolFilter(srv.filterToolsForIdentity),
	)
	srv.mcpServer = mcpSrv

	srv.registerResources()
	srv.registerTools()

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
func (s *Server) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// registerTools is implemented in tools.go (Task 10).
func (s *Server) registerTools() {}

// filterToolsForIdentity is wired as a WithToolFilter for tools/list. It
// receives the identity-carrying context from mcp-go (see WithHTTPContextFunc
// above) and is expected to drop tools the caller can't run. The real
// implementation lands in Task 10; the default is a pass-through so the
// constructor compiles without behavior change.
func (s *Server) filterToolsForIdentity(_ context.Context, tools []mcplib.Tool) []mcplib.Tool {
	return tools
}
