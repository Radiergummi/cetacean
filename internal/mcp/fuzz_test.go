package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// Drives arbitrary bytes at the MCP streamable HTTP transport as a JSON-RPC
// body, the boundary a client controls entirely. A malformed envelope is the
// client's fault and must come back as a JSON-RPC error, never a 5xx, and a 200
// carrying JSON must be a JSON-RPC envelope. SSE framing is tolerated.
func FuzzMCPEnvelope(f *testing.F) {
	srv, err := New(cache.New(nil), Options{
		Config:         config.MCPConfig{Enabled: true, OperationsLevel: config.OpsInherit},
		GlobalOpsLevel: config.OpsReadOnly,
		AuthMode:       "none",
	})
	if err != nil {
		f.Fatalf("construct server: %v", err)
	}
	f.Cleanup(srv.Close)

	handler := srv.Handler()

	seeds := [][]byte{
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`),
		[]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call"}`),
		[]byte(`{"jsonrpc":"2.0"}`),
		[]byte(`{"jsonrpc":"2.0","id":[1,2],"method":"tools/list"}`),
		[]byte(`[]`),
		[]byte(`null`),
		[]byte(``),
		[]byte(`{`),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set(mcplib.HeaderProtocolVersion, ProtocolVersion)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		resp := rec.Result()
		if resp.StatusCode >= http.StatusInternalServerError {
			t.Fatalf("got %d for input %q", resp.StatusCode, body)
		}

		raw := rec.Body.Bytes()
		contentType := resp.Header.Get("Content-Type")
		if resp.StatusCode == http.StatusOK && strings.Contains(contentType, "application/json") &&
			len(raw) > 0 {
			var envelope struct {
				JSONRPC string `json:"jsonrpc"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatalf("200 JSON response is not valid JSON: %v (%s)", err, raw)
			}
			if envelope.JSONRPC != "2.0" {
				t.Fatalf("200 JSON response is not a JSON-RPC envelope: %s", raw)
			}
		}
	})
}
