package mcp

import (
	"encoding/json"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// TestInitializeAdvertisesInstructionsAndDescription drives the real initialize
// handshake through the MCP HTTP handler and asserts the result carries both
// the server instructions and the implementation description. It checks against
// the production constants (single source of truth) and that they are non-empty,
// so the test fails if either option is dropped or the constant is blanked.
func TestInitializeAdvertisesInstructionsAndDescription(t *testing.T) {
	if mcpInstructions == "" || mcpDescription == "" {
		t.Fatal("mcpInstructions and mcpDescription must be non-empty")
	}

	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsReadOnly)
	t.Cleanup(srv.Close)

	handler := srv.Handler()

	initResp, env := mcpJSONRPC(t, handler, "", `{
		"jsonrpc":"2.0","id":1,"method":"initialize",
		"params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}
	}`)
	if initResp.StatusCode != 200 {
		t.Fatalf("initialize status = %d", initResp.StatusCode)
	}
	if env.Error != nil {
		t.Fatalf("initialize returned error: %+v", env.Error)
	}

	var r struct {
		Instructions string `json:"instructions"`
		ServerInfo   struct {
			Description string `json:"description"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(env.Result, &r); err != nil {
		t.Fatalf("decode initialize result: %v: %s", err, string(env.Result))
	}

	if r.Instructions != mcpInstructions {
		t.Errorf(
			"instructions mismatch:\n got: %q\nwant: %q",
			r.Instructions,
			mcpInstructions,
		)
	}
	if r.ServerInfo.Description != mcpDescription {
		t.Errorf(
			"serverInfo.description mismatch:\n got: %q\nwant: %q",
			r.ServerInfo.Description,
			mcpDescription,
		)
	}
}
