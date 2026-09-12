package contract

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/mcp"
	"github.com/radiergummi/cetacean/internal/mcp/oauth"
)

// Identifiers the fixture uses. Sweeps address resources through these rather
// than through literals, so a change to the fixture is a change in one place.
const (
	SeededServiceID   = "svc-shop-web"
	SeededServiceName = "shop_web"
	SeededNodeID      = "node-manager-1"
	SeededNodeName    = "manager-1"
	SeededTaskID      = "task-shop-web-1"
	SeededConfigID    = "cfg-shop"
	SeededSecretID    = "sec-shop"
	SeededNetworkID   = "net-shop"
	SeededVolumeName  = "vol-shop-data"
	SeededStack       = "shop"
)

// AnonymousPersona names the identity that carries no groups at all: the
// default-deny case every authorization sweep needs.
const AnonymousPersona = "anonymous"

// personas are the identities compose.dev-auth.yaml defines for the dashboard's
// own development environment, reused here rather than invented a second time —
// a finding in this package and a finding in the e2e ACL lane then describe the
// same people.
var personas = map[string]*auth.Identity{
	"ops": {
		Subject:  "alice",
		Email:    "alice@example.com",
		Groups:   []string{"ops"},
		Provider: "headers",
	},
	"viewers": {
		Subject:  "bob",
		Email:    "bob@example.com",
		Groups:   []string{"viewers"},
		Provider: "headers",
	},
	"frontend": {
		Subject:  "carol",
		Email:    "carol@example.com",
		Groups:   []string{"frontend"},
		Provider: "headers",
	},
	"oncall": {
		Subject:  "dave",
		Email:    "dave@example.com",
		Groups:   []string{"oncall"},
		Provider: "headers",
	},

	AnonymousPersona: {Subject: "eve", Email: "eve@example.com", Provider: "headers"},
}

// PersonaNames returns the persona keys, sorted, so a sweep over them reports
// in a stable order.
func PersonaNames() []string {
	return slices.Sorted(maps.Keys(personas))
}

// aclPolicy is the policy compose.dev-auth.yaml serves to its ACL lane, copied
// verbatim. Keeping the text identical is the point: the two environments grant
// the same people the same things.
const aclPolicy = `
grants:
  - resources: ["*"]
    audience: ["group:ops"]
    permissions: ["read", "write"]

  - resources: ["*"]
    audience: ["group:viewers"]
    permissions: ["read"]

  - resources: ["stack:frontend-*"]
    audience: ["group:frontend"]
    permissions: ["read", "write"]
  - resources: ["*"]
    audience: ["group:frontend"]
    permissions: ["read"]

  - resources: ["service:*", "task:*"]
    audience: ["group:oncall"]
    permissions: ["read", "write"]
  - resources: ["node:*", "swarm:*", "config:*", "secret:*", "network:*", "volume:*"]
    audience: ["group:oncall"]
    permissions: ["read"]
`

// World is one seeded cluster served over every transport Cetacean speaks.
type World struct {
	Cache  *cache.Cache
	Server *httptest.Server
	ACL    *acl.Evaluator
}

// NewWorld seeds a cluster and mounts the real REST router and the real MCP
// handler over it. Both read the same cache, which is what makes a
// cross-transport invariant meaningful: a divergence is the transports
// disagreeing, never two fixtures disagreeing.
func NewWorld(t *testing.T) *World {
	t.Helper()

	c := cache.New(nil)
	seedCluster(c)

	policy, err := acl.ParsePolicy([]byte(aclPolicy))
	if err != nil {
		t.Fatalf("parse the dev-auth policy: %v", err)
	}

	evaluator := acl.NewEvaluator()
	evaluator.SetResolver(c)
	evaluator.SetPolicy(policy)

	broadcaster := sse.NewBroadcaster(0, func(
		http.ResponseWriter, *http.Request, string, string,
	) {
	}, nil)
	t.Cleanup(broadcaster.Close)

	ready := make(chan struct{})
	close(ready)

	// Docker clients are nil: every read answers from the cache. A read
	// endpoint that needs a live daemon is an e2e-only route and is excused as
	// one, rather than stubbed into looking covered here.
	handlers := api.NewHandlers(
		c, broadcaster,
		nil, nil, nil, nil,
		ready,
		nil,
		config.OpsImpactful,
		nil,
		evaluator,
	)

	// mcp.New's Handler only installs its bearer-token middleware when OAuth
	// is non-nil (see internal/mcp/server.go's Handler doc comment), matching
	// main.go's own wiring: setupMCP always builds a real *oauth.Server
	// whenever the upstream auth mode is not "none", before calling mcp.New.
	// Without one here, /mcp would be served unauthenticated regardless of
	// the "headers" auth mode REST runs under — no identity would ever reach
	// checkRead, and every ACL-restrictive persona would read everything.
	// This Server never mints or verifies a token: AuthBypass below sends
	// every request through the upstream provider instead (see bypassActive
	// and bearerAuth's comment naming "headers" as one of the providers safe
	// for this, since it never writes on the success path), so the config
	// only needs to be valid enough for NewServer to construct.
	oauthSrv := oauth.NewServer(oauth.ServerConfig{
		Issuer:      "https://cetacean.test",
		MCPResource: "https://cetacean.test/mcp",
		MCP:         config.MCPConfig{Enabled: true, OperationsLevel: config.OpsInherit},
	})

	authProvider := headersProvider()

	mcpServer, err := mcp.New(c, mcp.Options{
		ACL: evaluator,
		Config: config.MCPConfig{
			Enabled:         true,
			OperationsLevel: config.OpsInherit,
			AuthBypass:      []string{"headers"},
		},
		GlobalOpsLevel: config.OpsImpactful,
		OAuth:          oauthSrv,
		AuthMode:       "headers",
		AuthProvider:   authProvider,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	t.Cleanup(mcpServer.Close)

	spa := api.NewSPAHandler(fs.FS(fstest.MapFS{
		"index.html": {Data: []byte("<html></html>")},
	}), "")

	router := api.NewRouter(api.RouterConfig{
		Handlers:       handlers,
		Broadcaster:    broadcaster,
		SPA:            spa,
		OpenAPISpec:    []byte("openapi: '3.1.0'"),
		AsyncAPISpec:   []byte("asyncapi: '3.0.0'"),
		AuthProvider:   authProvider,
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		MCPHandler:     mcpServer.Handler(),
	})

	server := httptest.NewServer(recordPatterns(router))
	t.Cleanup(server.Close)

	return &World{Cache: c, Server: server, ACL: evaluator}
}

// headersProvider builds the auth provider the world runs under. Headers mode
// is the one a test can drive per request, which is what lets one world answer
// for every persona.
func headersProvider() auth.Provider {
	return auth.NewHeadersProvider(config.HeadersConfig{
		Subject: "X-Auth-User",
		Email:   "X-Auth-Email",
		Groups:  "X-Auth-Groups",
	})
}

// REST issues a request as the named persona and returns the response. The
// caller closes the body.
func (w *World) REST(t *testing.T, persona, method, path string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, w.Server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	setPersonaHeaders(t, req, persona)

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}

	return resp
}

// RPCError is a JSON-RPC error envelope, returned rather than fataled: a tool
// refusing a call is an outcome an invariant asserts on.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP issues one JSON-RPC call as the named persona.
//
// The server speaks protocol 2026-07-28 only and is stateless: there is no
// initialize and no session id. Every modern request must declare its protocol
// version in the Mcp-Protocol-Version header and its client capabilities in
// params._meta, or mcp-go refuses it before any handler runs.
func (w *World) MCP(
	t *testing.T,
	persona, method string,
	params map[string]any,
) (json.RawMessage, *RPCError) {
	t.Helper()

	if params == nil {
		params = map[string]any{}
	}

	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    mcp.ProtocolVersion,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		t.Fatalf("marshal %s: %v", method, err)
	}

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, w.Server.URL+"/mcp", bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", mcp.ProtocolVersion)
	req.Header.Set("Mcp-Method", method)
	setPersonaHeaders(t, req, persona)

	// Mcp-Name is not always params.name: mcp-go keys resources/read on
	// params.uri instead, and some methods (tools/list among them) carry no
	// name header at all. ExtractHeaderName is mcp-go's own mapping, called
	// rather than mirrored, so this can't silently drift from it.
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params for %s: %v", method, err)
	}

	if name, ok := mcplib.ExtractHeaderName(mcplib.MCPMethod(method), paramsJSON); ok {
		req.Header.Set("Mcp-Name", name)
	}

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp %s: %v", method, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s: status = %d, body: %s", method, resp.StatusCode, body)
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode %s: %v", method, err)
	}

	if envelope.Error != nil {
		return nil, envelope.Error
	}

	return envelope.Result, nil
}

func setPersonaHeaders(t *testing.T, r *http.Request, persona string) {
	t.Helper()

	identity, ok := personas[persona]
	if !ok {
		t.Fatalf("unknown persona %q; have %v", persona, PersonaNames())
	}

	r.Header.Set("X-Auth-User", identity.Subject)
	r.Header.Set("X-Auth-Email", identity.Email)

	if len(identity.Groups) > 0 {
		r.Header.Set("X-Auth-Groups", strings.Join(identity.Groups, ","))
	}
}

// seedCluster fills the cache with one resource of every type. Names carry a
// stack prefix, as Docker's do, because that is what makes name resolution —
// and the REST/MCP divergence in it — observable at all.
func seedCluster(c *cache.Cache) {
	stackLabels := map[string]string{"com.docker.stack.namespace": SeededStack}

	c.SetNode(swarm.Node{
		ID:   SeededNodeID,
		Meta: swarm.Meta{Version: swarm.Version{Index: 3}},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleManager,
			Availability: swarm.NodeAvailabilityActive,
			Annotations:  swarm.Annotations{Labels: map[string]string{"zone": "eu-west"}},
		},
		Description:   swarm.NodeDescription{Hostname: SeededNodeName},
		ManagerStatus: &swarm.ManagerStatus{Leader: true},
	})

	c.SetService(swarm.Service{
		ID:   SeededServiceID,
		Meta: swarm.Meta{Version: swarm.Version{Index: 11}},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: SeededServiceName, Labels: stackLabels},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.27"},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: replicas(2)},
			},
		},
	})

	c.SetTask(swarm.Task{
		ID:        SeededTaskID,
		ServiceID: SeededServiceID,
		NodeID:    SeededNodeID,
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	c.SetConfig(swarm.Config{
		ID: SeededConfigID,
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{Name: "shop_app-config", Labels: stackLabels},
			Data:        []byte("key=value"),
		},
	})

	c.SetSecret(swarm.Secret{
		ID: SeededSecretID,
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "shop_app-secret", Labels: stackLabels},
			Data:        []byte("hunter2"),
		},
	})

	c.SetNetwork(network.Summary{
		ID:     SeededNetworkID,
		Name:   "shop_backend",
		Driver: "overlay",
		Labels: stackLabels,
	})

	c.SetVolume(volume.Volume{
		Name:      SeededVolumeName,
		Driver:    "local",
		CreatedAt: "2026-01-01T00:00:00Z",
		Labels:    stackLabels,
	})
}

func replicas(n uint64) *uint64 { return new(n) }
