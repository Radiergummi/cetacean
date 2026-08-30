package mcp

import (
	"encoding/json"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// TestDiscoverAdvertisesInstructionsAndDescription drives server/discover — the
// 2026-07-28 replacement for the initialize handshake — through the MCP HTTP
// handler and asserts the result carries both the server instructions and the
// implementation description. It checks against
// the production constants (single source of truth) and that they are non-empty,
// so the test fails if either option is dropped or the constant is blanked.
func TestDiscoverAdvertisesInstructionsAndDescription(t *testing.T) {
	if mcpInstructions == "" || mcpDescription == "" {
		t.Fatal("mcpInstructions and mcpDescription must be non-empty")
	}

	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsReadOnly)
	t.Cleanup(srv.Close)

	handler := srv.Handler()

	resp, env := mcpModern(t, handler, 1, "server/discover", `{}`)
	if resp.StatusCode != 200 {
		t.Fatalf("server/discover status = %d", resp.StatusCode)
	}

	if env.Error != nil {
		t.Fatalf("server/discover returned error: %+v", env.Error)
	}

	// 2026-07-28 moved the server implementation into the result's _meta.
	var r struct {
		Instructions string `json:"instructions"`
		Meta         struct {
			ServerInfo struct {
				Description string `json:"description"`
			} `json:"io.modelcontextprotocol/serverInfo"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(env.Result, &r); err != nil {
		t.Fatalf("decode server/discover result: %v: %s", err, string(env.Result))
	}

	if r.Instructions != mcpInstructions {
		t.Errorf(
			"instructions mismatch:\n got: %q\nwant: %q",
			r.Instructions,
			mcpInstructions,
		)
	}
	if r.Meta.ServerInfo.Description != mcpDescription {
		t.Errorf(
			"serverInfo.description mismatch:\n got: %q\nwant: %q",
			r.Meta.ServerInfo.Description,
			mcpDescription,
		)
	}
}
