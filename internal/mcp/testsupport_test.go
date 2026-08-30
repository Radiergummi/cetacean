package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
)

// newTestServer returns a Server backed by an empty cache and the default MCP
// config, for tests that care about protocol wiring rather than cluster data.
func newTestServer(t *testing.T) *Server {
	t.Helper()

	return newResourceTestServer(t, cache.New(nil))
}

// stubSession is the minimal ClientSession mcp-go needs to attribute a request
// to a session. It notifies nowhere: tests that assert on subscription
// bookkeeping never read the channel.
//
// mcp-go's own NewInProcessSession would also satisfy the interface, but it
// reports Initialized() false until Initialize() is called and drags in
// sampling/elicitation/roots machinery these tests do not exercise.
type stubSession struct {
	id string
}

func (s stubSession) SessionID() string { return s.id }
func (s stubSession) Initialize()       {}
func (s stubSession) Initialized() bool { return true }

func (s stubSession) NotificationChannel() chan<- mcplib.JSONRPCNotification {
	return make(chan mcplib.JSONRPCNotification, 1)
}

// contextWithSession stamps a session onto ctx the way mcp-go's transport does
// before it invokes a handler or a hook.
func contextWithSession(t *testing.T, srv *Server, sessionID string) context.Context {
	t.Helper()

	return srv.mcpServer.WithContext(context.Background(), stubSession{id: sessionID})
}

// mcpModern issues a request the way a 2026-07-28 client does: no session, no
// initialize handshake, and the protocol version declared both in _meta and in
// the Mcp-Protocol-Version header.
func mcpModern(
	t *testing.T,
	handler http.Handler,
	id int,
	method, params string,
) (mcpJSONRPCResult, jsonrpcEnvelope) {
	t.Helper()

	return sendMCP(t, handler, modernRequest(t, id, method, params))
}

// modernRequest builds the request mcpModern sends, for tests that need to add
// a header of their own before it goes out.
func modernRequest(t *testing.T, id int, method, params string) *http.Request {
	t.Helper()

	paramsJSON := withProtocolMeta(t, params)
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, id, method, paramsJSON)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(mcplib.HeaderProtocolVersion, mcplib.LATEST_PROTOCOL_VERSION)

	// SEP-2243 requires Mcp-Method, and Mcp-Name for the methods that address
	// something. Let mcp-go derive them: it knows which methods carry a name,
	// and it base64-encodes values that are not header-safe, which the server
	// decodes before comparing against the body.
	for key, value := range mcplib.StandardHeaders(
		mcplib.LATEST_PROTOCOL_VERSION,
		mcplib.MCPMethod(method),
		json.RawMessage(paramsJSON),
	) {
		req.Header.Set(key, value)
	}

	return req
}

// withProtocolMeta adds the per-request protocol metadata a modern client sends
// to a params object. 2026-07-28 removed the initialize handshake, so every
// request restates the protocol version, the client's identity, and its
// capabilities: the server must not infer any of them from a previous request.
func withProtocolMeta(t *testing.T, params string) json.RawMessage {
	t.Helper()

	object := map[string]any{}
	if trimmed := strings.TrimSpace(params); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &object); err != nil {
			t.Fatalf("params %q is not a JSON object: %v", params, err)
		}
	}

	object["_meta"] = map[string]any{
		mcplib.MetaKeyProtocolVersion:    mcplib.LATEST_PROTOCOL_VERSION,
		mcplib.MetaKeyClientInfo:         map[string]string{"name": "test", "version": "1.0"},
		mcplib.MetaKeyClientCapabilities: map[string]any{},
	}

	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	return raw
}
