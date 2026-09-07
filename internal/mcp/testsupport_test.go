package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/cache"
)

// callToolExpectingError is callTool's negative twin: it drives a real
// tools/call, requires the result to be a tool error, and returns the raw
// envelope so the caller can assert on the message.
//
// It exists because callTool fatals on IsError, so every refusal test was
// open-coding the transport call, the decode and the IsError check — including
// the load-bearing "not stubbed" assertion, which is how these tests show the
// refusal happened before the write client was ever touched.
func callToolExpectingError(t *testing.T, handler http.Handler, params string) string {
	t.Helper()

	_, envelope := mcpModern(t, handler, 1, "tools/call", params)
	if envelope.Error != nil {
		t.Fatalf("tools/call failed at the protocol level: %+v", envelope.Error)
	}

	var result toolCallResult
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode tool result: %v (raw %s)", err, envelope.Result)
	}

	if !result.IsError {
		t.Fatalf("expected a tool error, got: %s", envelope.Result)
	}

	raw := string(envelope.Result)

	// A refusal must happen before the Docker write client is reached; the
	// fakes stub nothing, so their panic message reaching the caller means the
	// tool wrote first and validated afterwards.
	if strings.Contains(raw, "not stubbed") {
		t.Errorf("the tool reached the write client before refusing: %s", raw)
	}

	return raw
}

// newTestServer returns a Server backed by an empty cache and the default MCP
// config, for tests that care about protocol wiring rather than cluster data.
func newTestServer(t *testing.T) *Server {
	t.Helper()

	return newResourceTestServer(t, cache.New(nil))
}

// stubSession is the minimal ClientSession mcp-go needs to attribute a request
// to a session. Its notification channel is buffered so a delivery never
// blocks; tests that assert on what reached a client read the real
// subscriptions/listen stream instead.
//
// mcp-go's own NewInProcessSession would also satisfy the interface, but it
// reports Initialized() false until Initialize() is called and drags in
// sampling/elicitation/roots machinery these tests do not exercise.
type stubSession struct {
	id            string
	notifications chan mcplib.JSONRPCNotification
}

func (s stubSession) SessionID() string { return s.id }
func (s stubSession) Initialize()       {}
func (s stubSession) Initialized() bool { return true }

func (s stubSession) NotificationChannel() chan<- mcplib.JSONRPCNotification {
	return s.notifications
}

// contextWithSession stamps a session onto ctx the way mcp-go's transport does
// before it invokes a handler or a hook. It goes through session() so the value
// on the context is the same one a later assertion looks up: NotificationManager
// keys on session identity, not on the session ID.
func contextWithSession(t *testing.T, srv *Server, sessionID string) context.Context {
	t.Helper()

	return srv.mcpServer.WithContext(context.Background(), session(sessionID))
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

// mcpModernWithToken is the bearer-aware variant, for tests that drive the
// OAuth-protected handler.
func mcpModernWithToken(
	t *testing.T,
	handler http.Handler,
	bearer string,
	id int,
	method, params string,
) (mcpJSONRPCResult, jsonrpcEnvelope) {
	t.Helper()

	req := modernRequest(t, id, method, params)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	return sendMCP(t, handler, req)
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

	// Merge rather than assign: a caller may have put its own keys in _meta
	// (SEP-414 trace context, say), and those must survive alongside the
	// protocol metadata every request carries.
	meta, _ := object["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}

	meta[mcplib.MetaKeyProtocolVersion] = mcplib.LATEST_PROTOCOL_VERSION
	meta[mcplib.MetaKeyClientInfo] = map[string]string{"name": "test", "version": "1.0"}
	meta[mcplib.MetaKeyClientCapabilities] = map[string]any{}
	object["_meta"] = meta

	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	return raw
}

// sessions memoises stub sessions by name so that repeated lookups for the same
// name yield the same value. NotificationManager keys on session identity, so a
// test that subscribes and then asserts must present the same session both
// times — as a real transport would.
var sessions sync.Map

// session returns the stub session for name, creating it on first use.
func session(name string) mcpserver.ClientSession {
	value, _ := sessions.LoadOrStore(name, stubSession{
		id:            name,
		notifications: make(chan mcplib.JSONRPCNotification, 16),
	})

	return value.(mcpserver.ClientSession)
}

// toolRequest builds the CallToolRequest a handler sees, so a tool test
// exercises the same argument decoding a real call does.
func toolRequest(t *testing.T, name string, args map[string]any) mcplib.CallToolRequest {
	t.Helper()

	var req mcplib.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args

	return req
}
