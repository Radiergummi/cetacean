//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// mcpCall issues one JSON-RPC request against the stateless MCP transport.
// Protocol 2026-07-28 has no handshake and no session id, but a modern
// request still needs the protocol version in both the Mcp-Protocol-Version
// header and params._meta, a clientCapabilities object, an Mcp-Method header
// mirroring the JSON-RPC method, and — for tools/call, resources/read and
// prompts/get — an Mcp-Name header matching params.name.
func mcpCall(
	t *testing.T,
	proc *sut.Process,
	method string,
	params map[string]any,
) json.RawMessage {
	t.Helper()

	if params == nil {
		params = map[string]any{}
	}

	// Client capabilities are required on every modern request too
	// (server/protocol.go's extractRequestProtocolInfo); an empty object is a
	// valid declaration of "no optional capabilities".
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		proc.BaseURL+"/mcp", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)

	if name, ok := params["name"].(string); ok {
		req.Header.Set("Mcp-Name", name)
	}

	resp, err := proc.Client().Do(req)
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
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode %s: %v", method, err)
	}

	if envelope.Error != nil {
		t.Fatalf("%s returned an error: %s", method, envelope.Error.Message)
	}

	return envelope.Result
}

// startMCP brings up the shared environment with CETACEAN_MCP=true and no
// auth, on the `none` lane's reserved port. Auth mode "none" means setupMCP
// never builds an OAuth server, so /mcp is reachable with a bare request.
func startMCP(t *testing.T) (*harness.Env, *sut.Process) {
	t.Helper()

	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	proc := sut.Start(t, sut.Config{
		Port:       19001,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE": "none",
			"CETACEAN_MCP":       "true",
		},
	})

	return env, proc
}

// TestMCPToolsListIsGatedByOperationsLevel checks tools/list at the default
// operations level (1, Operational). The assertion is two-sided on purpose:
// a tier-0 tool must be present and a tier-3 tool absent, where a one-sided
// absence check would pass vacuously on an empty or errored list.
func TestMCPToolsListIsGatedByOperationsLevel(t *testing.T) {
	_, proc := startMCP(t)

	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(
		mcpCall(t, proc, "tools/list", map[string]any{}),
		&listed,
	); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}

	if !names["find"] {
		t.Errorf("find missing from tools/list; got %v", names)
	}

	// Default operations level is 1, so tier-3 removals must not appear.
	if names["remove_service"] {
		t.Errorf("remove_service listed at the default operations level")
	}
}

// TestMCPFindAndDescribeAgree checks find and describe against the same live
// service: find's `type` is plural and its rows sit under "items", describe's
// is singular and its digest is inlined at the top level. The comparison is
// on State rather than identity, since State is computed independently on
// each path and so agreement is a genuine cross-check.
func TestMCPFindAndDescribeAgree(t *testing.T) {
	_, proc := startMCP(t)

	var found struct {
		StructuredContent struct {
			Items []struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				State string `json:"state"`
			} `json:"items"`
		} `json:"structuredContent"`
	}

	raw := mcpCall(t, proc, "tools/call", map[string]any{
		"name":      "find",
		"arguments": map[string]any{"type": "services"},
	})

	if err := json.Unmarshal(raw, &found); err != nil {
		t.Fatalf("decode find: %v", err)
	}

	if len(found.StructuredContent.Items) == 0 {
		t.Fatal("find returned no services")
	}

	first := found.StructuredContent.Items[0]

	var described struct {
		StructuredContent struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"structuredContent"`
	}

	raw = mcpCall(t, proc, "tools/call", map[string]any{
		"name":      "describe",
		"arguments": map[string]any{"type": "service", "id": first.ID},
	})

	if err := json.Unmarshal(raw, &described); err != nil {
		t.Fatalf("decode describe: %v", err)
	}

	if described.StructuredContent.ID != first.ID {
		t.Errorf("describe id = %q, find id = %q", described.StructuredContent.ID, first.ID)
	}

	if described.StructuredContent.Name != first.Name {
		t.Errorf("describe name = %q, find name = %q", described.StructuredContent.Name, first.Name)
	}

	if described.StructuredContent.State != first.State {
		t.Errorf(
			"describe state = %q, find state = %q; independently derived states disagree",
			described.StructuredContent.State, first.State,
		)
	}
}
