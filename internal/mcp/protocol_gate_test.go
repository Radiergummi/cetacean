package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// postRaw sends a body verbatim, without the modern metadata mcpModern adds, so
// a test can present exactly what an old client would.
func postRaw(
	t *testing.T,
	handler http.Handler,
	body string,
	headers map[string]string,
) (int, jsonrpcEnvelope) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	result, env := sendMCP(t, handler, req)

	return result.StatusCode, env
}

// TestLegacyInitializeIsRejected is the point of dropping legacy support: a
// client on an older revision must be told plainly, not served a session that
// cannot receive notifications.
func TestLegacyInitializeIsRejected(t *testing.T) {
	handler := newTestServer(t).Handler()

	status, env := postRaw(t, handler, `{
		"jsonrpc":"2.0","id":1,"method":"initialize",
		"params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"old","version":"1"}}
	}`, nil)

	if status == http.StatusOK && env.Error == nil {
		t.Fatal("legacy initialize was accepted; the server no longer supports that protocol era")
	}

	if env.Error == nil {
		t.Fatalf("expected a JSON-RPC error, got status %d", status)
	}

	if !strings.Contains(env.Error.Message, mcplib.LATEST_PROTOCOL_VERSION) {
		t.Errorf("error should name the required protocol version, got %q", env.Error.Message)
	}
}

// TestRequestWithoutProtocolVersionIsRejected covers the other legacy shape: a
// bare call that never declares a version at all.
func TestRequestWithoutProtocolVersionIsRejected(t *testing.T) {
	handler := newTestServer(t).Handler()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`

	status, env := postRaw(t, handler, body, nil)
	if env.Error == nil {
		t.Fatalf("undeclared protocol version was accepted (status %d)", status)
	}
}

// TestLegacySessionHeaderIsRejected — Mcp-Session-Id has no meaning any more,
// and honouring it would resurrect the session path we removed.
func TestLegacySessionHeaderIsRejected(t *testing.T) {
	handler := newTestServer(t).Handler()

	status, env := postRaw(t,
		handler,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		map[string]string{mcplib.HeaderSessionID: "some-old-session"},
	)
	if env.Error == nil {
		t.Fatalf("a request carrying Mcp-Session-Id was accepted (status %d)", status)
	}
}

// TestRejectionIsWellFormedJSONRPC — the rejection is the first thing an
// incompatible client sees, so it has to be parseable by that client.
func TestRejectionIsWellFormedJSONRPC(t *testing.T) {
	handler := newTestServer(t).Handler()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`,
	))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("rejection is not valid JSON: %v (body %q)", err, rec.Body.String())
	}

	if envelope.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", envelope.JSONRPC)
	}

	if string(envelope.ID) != "7" {
		t.Errorf("rejection dropped the request id: got %s, want 7", envelope.ID)
	}

	if envelope.Error == nil {
		t.Fatal("no error object in rejection")
	}
}

// TestModernRequestPassesTheGate guards against the gate rejecting what it
// should let through.
func TestModernRequestPassesTheGate(t *testing.T) {
	handler := newTestServer(t).Handler()

	resp, env := mcpModern(t, handler, 1, "tools/list", `{}`)
	if resp.StatusCode != http.StatusOK || env.Error != nil {
		t.Fatalf("modern request rejected: status %d, error %+v", resp.StatusCode, env.Error)
	}
}

// TestDiscoverAdvertisesOnlyTheSupportedVersion — server/discover is how a
// client learns what to speak. Advertising revisions the gate turns away would
// send a well-behaved client straight into a rejection.
func TestDiscoverAdvertisesOnlyTheSupportedVersion(t *testing.T) {
	handler := newTestServer(t).Handler()

	_, env := mcpModern(t, handler, 1, "server/discover", `{}`)
	if env.Error != nil {
		t.Fatalf("server/discover error: %+v", env.Error)
	}

	var result struct {
		SupportedVersions []string `json:"supportedVersions"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode server/discover: %v", err)
	}

	want := []string{mcplib.LATEST_PROTOCOL_VERSION}
	if !slices.Equal(result.SupportedVersions, want) {
		t.Fatalf("supportedVersions = %v, want %v", result.SupportedVersions, want)
	}
}
