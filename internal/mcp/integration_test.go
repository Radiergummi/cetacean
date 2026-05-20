package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
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
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data: ")) {
			return bytes.TrimPrefix(line, []byte("data: "))
		}
	}
	// Not SSE — assume raw JSON.
	return raw
}

func mcpJSONRPC(t *testing.T, handler http.Handler, sessionID, body string) (*http.Response, jsonrpcEnvelope) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	payload := readSSEResponse(t, resp.Body)

	var env jsonrpcEnvelope
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &env); err != nil {
			t.Fatalf("unmarshal JSON-RPC payload %q: %v", string(payload), err)
		}
	}
	return resp, env
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

	// 1) initialize
	initResp, env := mcpJSONRPC(t, handler, "", `{
		"jsonrpc":"2.0","id":1,"method":"initialize",
		"params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}
	}`)
	if initResp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d", initResp.StatusCode)
	}
	if env.Error != nil {
		t.Fatalf("initialize returned error: %+v", env.Error)
	}
	sessionID := initResp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initialize did not set Mcp-Session-Id")
	}

	// 2) resources/list — must include at least the static resources.
	_, env = mcpJSONRPC(t, handler, sessionID, `{
		"jsonrpc":"2.0","id":2,"method":"resources/list","params":{}
	}`)
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
	_, env = mcpJSONRPC(t, handler, sessionID, `{
		"jsonrpc":"2.0","id":3,"method":"resources/read",
		"params":{"uri":"cetacean://services/svc1"}
	}`)
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
	_, env = mcpJSONRPC(t, handler, sessionID, `{
		"jsonrpc":"2.0","id":4,"method":"tools/list","params":{}
	}`)
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
	_, env = mcpJSONRPC(t, handler, sessionID, `{
		"jsonrpc":"2.0","id":5,"method":"tools/call",
		"params":{"name":"search","arguments":{"query":"web"}}
	}`)
	if env.Error != nil {
		t.Fatalf("tools/call error: %+v", env.Error)
	}
	if !strings.Contains(string(env.Result), "web") {
		t.Errorf("search result missing web: %s", string(env.Result))
	}
}
