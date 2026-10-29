// Package mcp embeds a Model Context Protocol server in Cetacean. Resources
// expose cluster state and tools expose write operations to MCP-aware agents.
// It mounts behind the router's auth middleware; identity reaches mcp-go via
// WithHTTPContextFunc, and ACL is enforced per request inside each handler.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/core"
	"github.com/radiergummi/cetacean/internal/mcp/tracing"
	"github.com/radiergummi/cetacean/internal/recommendations"
	"github.com/radiergummi/cetacean/internal/version"
)

// TokenVerifier is the surface of the OAuth authorization server this transport
// consumes. It is auth.TokenVerifier because the API middleware needs exactly
// the same thing — the identity a bearer token carries, and the challenge that
// tells a client where to get one — and one contract cannot drift from itself.
type TokenVerifier = auth.TokenVerifier

// guardMode is what stands in front of /mcp. The endpoint is exempt from the
// API's auth middleware and authenticates itself, so this is the only thing
// between an anonymous caller and the cluster's write surface.
type guardMode int

const (
	// guardBearer verifies a token this deployment's authorization server
	// issued, falling back to the upstream provider for a bypassed mode.
	guardBearer guardMode = iota

	// guardUpstream authenticates every request through the upstream provider
	// alone. Reached when the active mode is bypassed and no authorization
	// server was built — the mTLS deployment, whose clients cannot drive a
	// browser consent screen and would leave one sitting unused.
	guardUpstream

	// guardNone serves unauthenticated, and is reachable only under an auth
	// mode that establishes no identity to begin with.
	guardNone
)

// resolveGuard settles what protects /mcp, refusing a configuration that would
// leave it open. It lives here rather than only in main's config validation
// because this package owns the endpoint. Serving unguarded takes an explicit
// "none"; an unset AuthMode is a caller that never settled the question.
func resolveGuard(opts Options) (guardMode, error) {
	if opts.OAuth != nil {
		// Both halves of the bearer guard are keyed by it: the audience a token
		// is verified against, and the metadata document the challenge names.
		if opts.Resource == "" {
			return guardNone, errors.New(
				"mcp: an authorization server needs the resource identifier it verifies " +
					"tokens against: without one /mcp would refuse every token and send " +
					"clients to another resource's metadata for a replacement",
			)
		}

		return guardBearer, nil
	}

	if opts.AuthMode == "none" {
		return guardNone, nil
	}

	if opts.AuthMode == "" {
		return guardNone, errors.New(
			"mcp: no auth mode was given, so nothing settles what guards /mcp",
		)
	}

	// No token to verify, so the upstream provider is the only thing that can
	// answer — and it may only answer for a mode the operator bypassed on
	// purpose.
	if !slices.Contains(opts.Config.AuthBypass, opts.AuthMode) {
		return guardNone, fmt.Errorf(
			"mcp: auth mode %q needs either an authorization server (oauth.enabled) "+
				"or %q in mcp.auth_bypass: without one of them /mcp would serve "+
				"unauthenticated",
			opts.AuthMode,
			opts.AuthMode,
		)
	}

	if opts.AuthProvider == nil {
		return guardNone, fmt.Errorf(
			"mcp: auth mode %q is bypassed but no auth provider was supplied, so "+
				"nothing would authenticate /mcp",
			opts.AuthMode,
		)
	}

	return guardUpstream, nil
}

// resolveBypass reports whether the upstream provider may establish identity
// ahead of the bearer check. Only guardBearer consults it: the other modes
// either run the provider for every request or authenticate nobody.
func resolveBypass(opts Options) bool {
	if opts.AuthProvider == nil || opts.AuthMode == "" {
		return false
	}

	return slices.Contains(opts.Config.AuthBypass, opts.AuthMode)
}

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

// DockerWriteClient is the Docker writer behind the MCP tools: exactly the
// surface the write use cases call. docker.Client satisfies it.
type DockerWriteClient interface {
	cluster.Writer
}

// RecommendationEngine is the narrow surface of the recommendations engine
// consumed by the MCP server. *recommendations.Engine satisfies it; passing
// nil (CETACEAN_RECOMMENDATIONS=false) is also acceptable.
type RecommendationEngine interface {
	Results() []recommendations.Recommendation
}

// MountPath is where the endpoint is served under the deployment's base path.
// It is also this transport's protected resource, so a token for the deployment
// root does not open it.
const MountPath = "/mcp"

// Server is the Cetacean MCP server.
type Server struct {
	cache          *cache.Cache
	versions       *cluster.VersionIndex
	writeClient    DockerWriteClient
	writes         *cluster.Writes
	logs           LogStreamer
	acl            *acl.Evaluator
	config         config.MCPConfig
	globalOpsLevel config.OperationsLevel
	oauth          TokenVerifier // nil unless an OAuth server was configured

	// resource is this transport's resource identifier, resolved once from the
	// verifier for the same reason guard is: which audience /mcp accepts is a
	// property of the deployment, not of a request.
	resource     string
	authMode     string        // upstream auth mode ("cert", "oidc", ...); used for bypass match
	authProvider auth.Provider // upstream auth provider; used when bypass is active

	// guard and bypass are settled in New from the configuration, not
	// re-derived per request. Who authenticates /mcp is a property of the
	// deployment, and asking a helper at request time is how an endpoint ends
	// up unguarded because a provider happened to be nil.
	guard        guardMode
	bypass       bool
	mcpServer    *mcpserver.MCPServer
	httpServer   *mcpserver.StreamableHTTPServer
	recEngine    RecommendationEngine
	prom         MetricsQuerier            // nil when Prometheus is not configured
	images       cluster.ImageDigests      // nil when server.image_check is off
	attestations cluster.AttestationSource // nil when server.image_attestations is off

	// allowedOrigins is the set of origins, serialised, the streamable HTTP
	// endpoint accepts. originGuard rejects any other non-empty Origin with
	// 403 (DNS-rebinding defense).
	allowedOrigins []string

	// hosts are the names hostGuard admits on a loopback connection.
	hosts auth.HostSet

	// iconBaseURL is the canonical external base (issuer + base path) that tool
	// icon `src` values are built from, e.g. "https://cetacean.example.com".
	// Icons live under the un-authed /assets/ prefix. Empty disables icons.
	iconBaseURL string

	// registeredTools is the subset of toolCatalog() the server actually wired
	// into mcp-go after applying the operations-level tier filter.
	registeredTools []toolDef

	// toolGates holds every catalogued tool's gate by name, registered or
	// not, for the per-identity tools/list and prompts/list filters.
	toolGates map[string]toolGate

	// registeredPrompts holds the prompts that passed the tier gate, so the
	// per-identity prompts/list filter (filterPromptsForIdentity) can read
	// each one's driven tools back.
	registeredPrompts []promptDef

	// notifications tracks per-session resource subscriptions and fans cache
	// events out as MCP notifications. cancelNotifications is the listener
	// detach returned by cache.AddOnChangeListener.
	notifications       *NotificationManager
	cancelNotifications func()

	// addressNetworks names the networks an event may move the address figure
	// of; nil is cluster.AddressNetworks.
	addressNetworks func(*cache.Cache, cache.Event) []string

	// taskOwners binds each task to the identity that created it.
	taskOwners taskOwners

	// watches bounds concurrent `watch` waits — see maxConcurrentWatches. A nil
	// channel disables the bound, which is what a Server built without New gets;
	// only New wires it, and only New serves real traffic.
	watches chan struct{}

	// listens bounds open subscriptions/listen streams — see maxListenStreams.
	// Nil disables the bound, as for watches.
	listens chan struct{}

	// streams ends the open listen streams at shutdown.
	streams *sse.Lifetime

	// logReads bounds log reads in flight — see logs.MaxConcurrentReads.
	logReads *sse.Slots

	// inflight scopes notifications/cancelled to the caller's own requests.
	inflight inflight

	// confirmations signs the requestState of a confirmation round trip.
	confirmations *stateSigner

	secretEntries *cluster.SecretEntries

	// widgets are the MCP Apps bundles this server serves.
	widgets      []uiResource
	dashboardURL string

	closeOnce sync.Once
}

// Options bundles the dependencies for New. A nil OAuth mounts /mcp without
// bearer validation, which is only safe in auth mode "none"; Recommendations
// is nil when the engine is off. When AuthMode appears in Config.AuthBypass,
// identity comes from AuthProvider instead of an OAuth bearer.
type Options struct {
	WriteClient    DockerWriteClient
	Logs           LogStreamer
	ACL            *acl.Evaluator
	Config         config.MCPConfig
	GlobalOpsLevel config.OperationsLevel
	OAuth          TokenVerifier

	// Resource is the RFC 8707 identifier of this transport's protected
	// resource, which OAuth verifies tokens against. Required with OAuth.
	Resource string

	AuthMode        string
	AuthProvider    auth.Provider
	Recommendations RecommendationEngine
	AllowedOrigins  []string

	// PublicURL is server.public_url. Its origin is admitted beside
	// AllowedOrigins, as the API's cross-origin guard admits it.
	PublicURL string

	// Hosts are the names the REST edge answers for, which hostGuard admits
	// on a loopback connection as well.
	Hosts auth.HostSet

	// Prometheus backs the get_metrics tool. Nil when CETACEAN_PROMETHEUS_URL
	// is unset, and the tool then says metrics are unavailable rather than
	// charting nothing.
	Prometheus MetricsQuerier

	// ImageDigests reports a newer image for a service's tag. Nil when
	// server.image_check is off.
	ImageDigests cluster.ImageDigests

	// Attestations reads the provenance and SBOMs of an image from its
	// registry. Nil when server.image_attestations is off.
	Attestations cluster.AttestationSource

	// IconBaseURL is the canonical external base URL (issuer + base path) used
	// to build absolute tool-icon `src` values. Icons are served from the
	// embedded frontend under /assets/mcp-icons/. Empty disables tool icons.
	IconBaseURL string

	// Tracer records spans for dispatched MCP methods and tool calls. Nil
	// leaves mcp-go's noop tracer in place, which is the zero-config path:
	// tracing costs nothing until CETACEAN_OTEL_ENDPOINT is set.
	Tracer oteltrace.Tracer

	// DashboardURL is the dashboard's external base, where create_secret
	// sends a person to enter a secret. Unset turns that off.
	DashboardURL string

	// Widgets holds the built MCP Apps bundles, one directory per widget. Nil
	// or empty serves none and leaves the UI extension unadvertised.
	Widgets fs.FS
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
func New(shared *core.Core, opts Options) (*Server, error) {
	if shared == nil || shared.Cache == nil {
		return nil, errors.New("mcp: shared state with a cache is required")
	}

	guard, err := resolveGuard(opts)
	if err != nil {
		return nil, err
	}

	srv := &Server{
		confirmations:  newStateSigner(),
		secretEntries:  shared.SecretEntries,
		dashboardURL:   strings.TrimRight(opts.DashboardURL, "/"),
		guard:          guard,
		bypass:         resolveBypass(opts),
		cache:          shared.Cache,
		writeClient:    opts.WriteClient,
		writes:         cluster.NewWrites(shared.Cache, opts.WriteClient),
		logs:           opts.Logs,
		acl:            opts.ACL,
		config:         opts.Config,
		globalOpsLevel: opts.GlobalOpsLevel,
		oauth:          opts.OAuth,
		resource:       opts.Resource,
		authMode:       opts.AuthMode,
		authProvider:   opts.AuthProvider,
		recEngine:      opts.Recommendations,
		prom:           opts.Prometheus,
		images:         opts.ImageDigests,
		attestations:   opts.Attestations,
		allowedOrigins: withPublicOrigin(opts.AllowedOrigins, opts.PublicURL),
		hosts:          opts.Hosts,
		iconBaseURL:    strings.TrimRight(opts.IconBaseURL, "/"),
		notifications:  NewNotificationManager(),
		versions:       shared.Versions,
		watches:        make(chan struct{}, maxConcurrentWatches),
		listens:        make(chan struct{}, maxListenStreams),
		streams:        shared.Streams,
		logReads:       shared.LogReads,
		widgets:        uiResources(opts.Widgets),
	}
	srv.notifications.resolve = srv.subscriptionFor
	srv.notifications.keys = srv.eventKeys

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
		mcpserver.WithExtensions(serverExtensions(len(srv.widgets) > 0)),
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
		mcpserver.WithTaskHooks(srv.taskCreationHooks()),
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
		version.Version,
		serverOptions...,
	)
	srv.mcpServer = mcpSrv

	srv.registerResources()
	srv.registerUIResources()
	srv.registerTools()
	srv.registerPrompts()
	srv.cancelNotifications = srv.startNotifications()

	httpSrv := mcpserver.NewStreamableHTTPServer(mcpSrv,
		// Protocol 2026-07-28 has no sessions, and requireSupportedProtocol
		// holds 2025-11-25 to its sessionless subset, so there is no session
		// state to keep: each request is served by its own ephemeral session.
		// WithStateful(false) would not do: it leaves mcp-go minting IDs.
		mcpserver.WithStateLess(true),
		// hostGuard replaces this check: it also admits server.public_url's
		// host, which a same-host proxy that keeps the public Host sends.
		mcpserver.WithDisableLocalhostProtection(true),
		mcpserver.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			// The bearer-validating middleware (see Handler) stamps an
			// auth.Identity on r.Context() before mcp-go sees the request.
			// Bridge it into the MCP-handler context so resources/tools can
			// look it up via auth.IdentityFromContext.
			if id := auth.IdentityFromContext(r.Context()); id != nil {
				ctx = cluster.Attributed(auth.ContextWithIdentity(ctx, id), cache.TransportMCP)
			}

			ctx = context.WithValue(ctx, remoteAddrKey{}, r.RemoteAddr)

			// server/discover reports this as supportedVersions. mcp-go would
			// otherwise list every revision it implements, advertising eras
			// requireSupportedProtocol turns away and sending a well-behaved
			// client straight into a rejection.
			return mcpserver.WithSupportedProtocolVersions(ctx, SupportedProtocolVersions)
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

// Handler returns the http.Handler for the MCP endpoint. The guard decides who
// authenticates the caller: the authorization server's own bearer tokens, the
// upstream provider the deployment already runs, or nobody — the last only
// where startup validation has established that auth mode is "none".
func (s *Server) Handler() http.Handler {
	// The body cap, protocol gate and listen cap sit inside the origin and
	// bearer checks so those answer first: an unauthenticated caller learns
	// nothing about what we speak, and gets no body buffered.
	h := limitBody(s.requireSupportedProtocol(s.limitListens(s.scopeCancels(s.httpServer))))

	switch s.guard {
	case guardBearer:
		h = s.bearerAuth(h)
	case guardUpstream:
		h = s.upstreamAuth(h)
	case guardNone:
	}

	return s.originGuard(s.hostGuard(h))
}

// hostGuard refuses, on a loopback connection, a Host the REST edge would
// refuse too, as mcp-go's own DNS-rebinding check does: a same-host reverse
// proxy dials loopback and may keep the public Host.
func (s *Server) hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
		if local == nil || !auth.IsLoopbackHost(auth.HostName(local.String())) ||
			s.hosts.Admits(r.Host) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "forbidden host", http.StatusForbidden)
	})
}

// originGuard is the DNS-rebinding defense the Streamable HTTP transport
// requires. It reads Origin, not Host, so it holds behind a proxy. A request
// with no Origin passes. Origins match as RFC 6454 serialises them, so "*"
// admits nothing: a wildcard is not trusted with POSTs that may write.
func (s *Server) originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if origin == "" || slices.Contains(s.allowedOrigins, serialiseOrigin(origin)) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "forbidden origin", http.StatusForbidden)
	})
}

// withPublicOrigin serialises the allowed origins and adds publicURL's. Config
// validation has already refused a public URL with a path, userinfo or a
// scheme other than http or https.
func withPublicOrigin(allowed []string, publicURL string) []string {
	origins := make([]string, 0, len(allowed)+1)
	for _, origin := range allowed {
		origins = append(origins, serialiseOrigin(origin))
	}

	u, err := url.Parse(publicURL)
	if publicURL == "" || err != nil || u.Host == "" {
		return origins
	}

	return append(origins, serialiseOrigin(u.Scheme+"://"+u.Host))
}

// serialiseOrigin is RFC 6454's serialisation of a scheme://host[:port]
// origin: scheme and host lower-cased, the scheme's default port dropped.
// Anything else, "null" and "*" among them, is returned as written.
func serialiseOrigin(origin string) string {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Opaque != "" ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return origin
	}

	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}

	switch port := u.Port(); {
	case port == "", scheme == "http" && port == "80", scheme == "https" && port == "443":
		return scheme + "://" + host
	default:
		return scheme + "://" + host + ":" + port
	}
}

// bearerAuth validates Authorization: Bearer <jwt>, puts an auth.Identity in
// the request context, and answers 401 with RFC 6750 WWW-Authenticate
// otherwise. When Config.AuthBypass names the active upstream mode, that
// provider runs first and its identity skips JWT verification.
func (s *Server) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.bypass {
			if id := s.upstreamIdentity(r); id != nil {
				ctx := auth.ContextWithIdentity(r.Context(), id)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		token := auth.ExtractBearerToken(r)
		if token == "" {
			s.writeUnauthorized(w, "")
			return
		}

		identity, err := s.oauth.Identify(token, s.resource)
		if err != nil {
			s.writeUnauthorized(w, "invalid_token")
			return
		}

		ctx, cancel := auth.ContextUntilExpiry(r.Context(), identity)
		defer cancel()

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// writeUnauthorized answers a missing or unusable bearer token with a bare 401
// carrying the challenge, since JSON-RPC has no envelope for a transport-level
// refusal. An empty errorCode omits the error parameter, as RFC 6750 §3.1 asks
// when the request carried no credential.
func (s *Server) writeUnauthorized(w http.ResponseWriter, errorCode string) {
	w.Header().Set("WWW-Authenticate", s.oauth.UnauthorizedHeader(s.resource, errorCode))
	w.WriteHeader(http.StatusUnauthorized)
}

// upstreamAuth authenticates every request through the upstream provider, for a
// deployment that bypasses the bearer check and runs no authorization server.
// The refusal is 403: the credential this accepts rides the transport below
// HTTP, so no challenge could ask for it, and 401 owes one.
func (s *Server) upstreamAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := s.upstreamIdentity(r)
		if id == nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
	})
}

// upstreamIdentity runs the upstream provider for its identity alone. A nil
// identity with no error means the provider wrote its own response — a
// redirect, say — into the writer that discards it. It established nothing, so
// it is a refusal like any other, and both callers treat it as one.
func (s *Server) upstreamIdentity(r *http.Request) *auth.Identity {
	id, err := s.authProvider.Authenticate(newDiscardingResponseWriter(), r)
	if err != nil {
		return nil
	}

	return id
}

// discardingResponseWriter swallows writes from a provider invoked only for
// its identity; config.bypassableAuthModes limits the list to providers that
// establish identity without writing. Header() is persistent, so a provider
// setting a header and reading it back sees itself. The zero value is unusable.
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
			gate := s.toolGates[name]
			if gate == (toolGate{}) {
				return true
			}

			return access.Can(gate.permission, gate.resourceType)
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

	return filterToolsByGate(tools, visibility.allow)
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

func filterToolsByGate(tools []mcplib.Tool, allow func(name string) bool) []mcplib.Tool {
	out := make([]mcplib.Tool, 0, len(tools))
	for _, t := range tools {
		if allow(t.Name) {
			out = append(out, t)
		}
	}
	return out
}
