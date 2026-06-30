package mcp

import (
	"encoding/json"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// wantInstructions and wantDescription are the server-level usage contract the
// initialize handshake must advertise to MCP clients.
const (
	wantInstructions = "Cetacean is a read-mostly observability and operations interface for a Docker Swarm cluster. Resolve a resource's ID or name with the search tool before reading its details or applying a write. Reads (the cetacean:// resources and the get_logs/search tools) are always available; mutating tools are gated by an operations tier and per-resource ACL, and may be hidden from tools/list or rejected at call time. Prefer the cetacean:// resources for detail and cross-references; use tools to change cluster state. Tool results include structuredContent you can parse directly."
	wantDescription  = "Read and safely operate a Docker Swarm cluster."
)

// TestInitializeAdvertisesInstructionsAndDescription drives the real initialize
// handshake through the MCP HTTP handler and asserts the result carries both
// the server instructions and the implementation description.
func TestInitializeAdvertisesInstructionsAndDescription(t *testing.T) {
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

	if r.Instructions != wantInstructions {
		t.Errorf(
			"instructions mismatch:\n got: %q\nwant: %q",
			r.Instructions,
			wantInstructions,
		)
	}
	if r.ServerInfo.Description != wantDescription {
		t.Errorf(
			"serverInfo.description mismatch:\n got: %q\nwant: %q",
			r.ServerInfo.Description,
			wantDescription,
		)
	}
}
