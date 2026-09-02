package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/mcp/oauth"
)

// jsonrpcEnvelope captures the subset of a JSON-RPC response we assert on.
type jsonrpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// readSSEResponse extracts the JSON payload from an SSE response body. mcp-go
// upgrades JSON-RPC responses to text/event-stream; each event has a `data:`
// line carrying the actual response object.
func readSSEResponse(t *testing.T, body io.Reader) []byte {
	t.Helper()
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
			return rest
		}
	}
	// Not SSE — assume raw JSON.
	return raw
}

// mcpJSONRPCResult captures the subset of the *http.Response the integration
// tests actually look at. Returning a value type instead of *http.Response
// keeps the response body lifecycle inside the helper (which always drains
// and closes it) and stops golangci-lint's bodyclose from flagging every
// caller.
type mcpJSONRPCResult struct {
	StatusCode int
	Header     http.Header
}

// sendMCP serves req and decodes the JSON-RPC envelope out of the response,
// whether it arrived as SSE or as plain JSON. It owns the response body's
// lifecycle — always draining and closing it — which is why callers get a value
// type rather than the *http.Response (and why bodyclose has nothing to flag).
func sendMCP(
	t *testing.T,
	handler http.Handler,
	req *http.Request,
) (mcpJSONRPCResult, jsonrpcEnvelope) {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	payload := readSSEResponse(t, resp.Body)
	_ = resp.Body.Close()

	var env jsonrpcEnvelope
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &env); err != nil {
			t.Fatalf("unmarshal JSON-RPC payload %q: %v", string(payload), err)
		}
	}

	return mcpJSONRPCResult{StatusCode: resp.StatusCode, Header: resp.Header}, env
}

// TestMCPEndToEnd drives the MCP HTTP handler through the same JSON-RPC
// sequence a real client would use: initialize → resources/list →
// resources/read. It exercises the full mcp-go pipeline including session
// creation, the WithHTTPContextFunc bridge, and resource handler dispatch.
func TestMCPEndToEnd(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "manager-1"},
	})

	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	srv, err := New(c, Options{
		Config:         cfg,
		GlobalOpsLevel: config.OpsReadOnly,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(srv.Close)

	handler := srv.Handler()

	// 1) server/discover — the 2026-07-28 replacement for initialize.
	discoverResp, env := mcpModern(t, handler, 1, "server/discover", `{}`)
	if discoverResp.StatusCode != http.StatusOK {
		t.Fatalf("server/discover status = %d", discoverResp.StatusCode)
	}

	if env.Error != nil {
		t.Fatalf("server/discover returned error: %+v", env.Error)
	}

	// 2) resources/list — must include at least the static resources.
	_, env = mcpModern(t, handler, 2, "resources/list", `{}`)
	if env.Error != nil {
		t.Fatalf("resources/list error: %+v", env.Error)
	}
	var listResult struct {
		Resources []struct {
			URI  string `json:"uri"`
			Name string `json:"name"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(env.Result, &listResult); err != nil {
		t.Fatalf("decode resources/list result: %v: %s", err, string(env.Result))
	}
	want := map[string]bool{
		"cetacean://cluster":         false,
		"cetacean://recommendations": false,
		"cetacean://history":         false,
	}
	for _, r := range listResult.Resources {
		if _, ok := want[r.URI]; ok {
			want[r.URI] = true
		}
	}
	for uri, found := range want {
		if !found {
			t.Errorf("resources/list missing %s", uri)
		}
	}

	// 3) resources/read for the service we seeded.
	_, env = mcpModern(t, handler, 3, "resources/read", `{"uri":"cetacean://services/svc1"}`)
	if env.Error != nil {
		t.Fatalf("resources/read error: %+v", env.Error)
	}
	var readResult struct {
		Contents []struct {
			URI      string `json:"uri"`
			MIMEType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(env.Result, &readResult); err != nil {
		t.Fatalf("decode resources/read result: %v: %s", err, string(env.Result))
	}
	if len(readResult.Contents) != 1 {
		t.Fatalf("expected 1 content entry, got %d", len(readResult.Contents))
	}
	if !strings.Contains(readResult.Contents[0].Text, `"Name":"web"`) {
		t.Errorf("service content missing Name=web: %s", readResult.Contents[0].Text)
	}

	// 4) tools/list — should include the read-only tools at OpsReadOnly tier.
	_, env = mcpModern(t, handler, 4, "tools/list", `{}`)
	if env.Error != nil {
		t.Fatalf("tools/list error: %+v", env.Error)
	}
	var toolsResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(env.Result, &toolsResult); err != nil {
		t.Fatalf("decode tools/list result: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range toolsResult.Tools {
		names[tl.Name] = true
	}
	if !names["search"] {
		t.Error("tools/list missing search")
	}
	if names["scale_service"] {
		t.Error("scale_service should be filtered out at OpsReadOnly")
	}

	// 5) tools/call for search — should hit the seeded service.
	_, env = mcpModern(t, handler, 5, "tools/call", `{"name":"search","arguments":{"query":"web"}}`)
	if env.Error != nil {
		t.Fatalf("tools/call error: %+v", env.Error)
	}
	if !strings.Contains(string(env.Result), "web") {
		t.Errorf("search result missing web: %s", string(env.Result))
	}
}

// newOAuthIntegrationServer wires an MCP server with OAuth + ACL identical to
// production. Returns the handler, a TokenIssuer for minting bearer tokens, and
// the OAuth server (kept for cleanup / future assertions).
func newOAuthIntegrationServer(
	t *testing.T,
	c *cache.Cache,
	e *acl.Evaluator,
) (http.Handler, *oauth.TokenIssuer) {
	t.Helper()

	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	key := []byte("integration-test-signing-key-32B!")

	oauthSrv := oauth.NewServer(oauth.ServerConfig{
		Issuer:      "https://cetacean.example.com",
		BasePath:    "",
		MCPResource: "https://cetacean.example.com/mcp",
		MCP:         cfg,
		SigningKey:  key,
	})

	srv, err := New(c, Options{
		Config:         cfg,
		GlobalOpsLevel: config.OpsReadOnly,
		OAuth:          oauthSrv,
		ACL:            e,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(srv.Close)

	issuer := &oauth.TokenIssuer{
		SigningKey: key,
		Issuer:     "https://cetacean.example.com",
		Audience:   "https://cetacean.example.com/mcp",
	}
	return srv.Handler(), issuer
}

// TestMCPIntegration_ResourcesReadHonoursACL drives resources/read through the
// full bearer-auth + JSON-RPC pipeline and confirms ACL denial reaches the
// caller as a JSON-RPC error (covers M-01 at HTTP level).
func TestMCPIntegration_ResourcesReadHonoursACL(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc-public",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "public-api"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc-secret",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "secret-svc"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:public-*"},
		Audience:    []string{"user:agent@example.com"},
		Permissions: []string{"read"},
	}}})

	handler, issuer := newOAuthIntegrationServer(t, c, e)

	token, err := issuer.IssueAccessToken(
		oauth.AccessTokenClaims{Subject: "agent@example.com"},
		5*time.Minute,
	)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	// Allowed — public-api is in the policy.
	_, env := mcpModernWithToken(t, handler, token, 2, "resources/read",
		`{"uri":"cetacean://services/svc-public"}`)
	if env.Error != nil {
		t.Fatalf("allowed read produced error: %+v", env.Error)
	}
	if !strings.Contains(string(env.Result), "public-api") {
		t.Errorf("expected public-api in result, got %s", string(env.Result))
	}

	// Denied — secret-svc is outside the policy.
	_, env = mcpModernWithToken(t, handler, token, 3, "resources/read",
		`{"uri":"cetacean://services/svc-secret"}`)
	if env.Error == nil {
		t.Fatalf(
			"denied read should have returned a JSON-RPC error, got result %s",
			string(env.Result),
		)
	}
	if !strings.Contains(env.Error.Message, "denied") {
		t.Errorf("error = %q, want a denial message", env.Error.Message)
	}
}

// TestMCPIntegration_SearchToolFiltersByACL drives the search tool through
// tools/call and confirms denied resources are filtered out before reaching
// the caller (covers M-02 at HTTP level).
func TestMCPIntegration_SearchToolFiltersByACL(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc-public",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "public-acme"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc-secret",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "secret-acme"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:public-*"},
		Audience:    []string{"user:agent@example.com"},
		Permissions: []string{"read"},
	}}})

	handler, issuer := newOAuthIntegrationServer(t, c, e)

	token, err := issuer.IssueAccessToken(
		oauth.AccessTokenClaims{Subject: "agent@example.com"},
		5*time.Minute,
	)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	// "acme" matches both services by name; ACL must hide the secret one.
	_, env := mcpModernWithToken(t, handler, token, 2, "tools/call",
		`{"name":"search","arguments":{"query":"acme"}}`)
	if env.Error != nil {
		t.Fatalf("search returned error: %+v", env.Error)
	}
	result := string(env.Result)
	if !strings.Contains(result, "public-acme") {
		t.Errorf("expected public-acme in result, got %s", result)
	}
	if strings.Contains(result, "secret-acme") {
		t.Errorf("ACL-denied service leaked into search result: %s", result)
	}
}

// TestMCPIntegration_UnauthorizedHeaderUsesRFC7230Quoting confirms the 401
// emitted on a missing bearer follows RFC 7230 quoted-string rules (covers
// M-10 at HTTP level).
func TestMCPIntegration_UnauthorizedHeaderUsesRFC7230Quoting(t *testing.T) {
	handler, _ := newOAuthIntegrationServer(t, cache.New(nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	www := rec.Header().Get("WWW-Authenticate")

	// Spec-compliant quoting wraps each value in plain "..." with no
	// Go-syntax escapes for legal characters. The PRM URL has no special
	// characters, so the rendered form must contain the URL verbatim
	// between double quotes.
	want := `resource_metadata="https://cetacean.example.com/.well-known/oauth-protected-resource"`
	if !strings.Contains(www, want) {
		t.Errorf("WWW-Authenticate = %q, want substring %q", www, want)
	}
	if strings.Contains(www, `\"`) {
		t.Errorf("WWW-Authenticate contains Go-style escaped quotes: %q", www)
	}
}
