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
// initialize handshake, the protocol version declared both in _meta and in the
// Mcp-Protocol-Version header, and the SEP-2243 standard request headers on
// every POST.
func mcpModern(
	t *testing.T,
	handler http.Handler,
	id int,
	method, params string,
) (mcpJSONRPCResult, jsonrpcEnvelope) {
	t.Helper()

	paramsJSON := withProtocolMeta(params)
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, id, method, paramsJSON)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", mcplib.LATEST_PROTOCOL_VERSION)
	req.Header.Set("Mcp-Method", method)

	// SEP-2243: Mcp-Name carries the name of the thing being addressed — the
	// tool name, or the resource URI — and the server rejects a value that
	// disagrees with the body. Methods that address nothing send no header.
	if name, ok := mcplib.ExtractHeaderName(mcplib.MCPMethod(method), json.RawMessage(paramsJSON)); ok {
		req.Header.Set("Mcp-Name", name)
	}

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

// withProtocolMeta splices the protocol-version _meta key into a params object.
func withProtocolMeta(params string) string {
	// 2026-07-28 removed the initialize handshake, so every request restates
	// the protocol version, the client's identity, and its capabilities: the
	// server must not infer any of them from a previous request.
	meta := fmt.Sprintf(
		`"_meta":{%q:%q,%q:{"name":"test","version":"1.0"},%q:{}}`,
		mcplib.MetaKeyProtocolVersion, mcplib.LATEST_PROTOCOL_VERSION,
		mcplib.MetaKeyClientInfo,
		mcplib.MetaKeyClientCapabilities,
	)

	trimmed := strings.TrimSpace(params)
	if trimmed == "" || trimmed == "{}" {
		return "{" + meta + "}"
	}

	return "{" + meta + "," + strings.TrimPrefix(trimmed, "{")
}
