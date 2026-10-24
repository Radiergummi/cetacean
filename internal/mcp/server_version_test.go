package mcp

import (
	"encoding/json"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/version"
)

// The serverInfo version is the build's, the same one REST reports.
func TestDiscoverReportsTheBuildVersion(t *testing.T) {
	previous := version.Version
	version.Version = "v9.8.7-test"
	t.Cleanup(func() { version.Version = previous })

	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsReadOnly)
	t.Cleanup(srv.Close)

	_, env := mcpModern(t, srv.Handler(), 1, "server/discover", `{}`)
	if env.Error != nil {
		t.Fatalf("server/discover returned error: %+v", env.Error)
	}

	var r struct {
		Meta struct {
			ServerInfo struct {
				Version string `json:"version"`
			} `json:"io.modelcontextprotocol/serverInfo"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(env.Result, &r); err != nil {
		t.Fatalf("decode server/discover result: %v: %s", err, env.Result)
	}

	if r.Meta.ServerInfo.Version != "v9.8.7-test" {
		t.Errorf("serverInfo.version = %q, want the build version", r.Meta.ServerInfo.Version)
	}
}
