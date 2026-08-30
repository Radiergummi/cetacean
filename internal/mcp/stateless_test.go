package mcp

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestModernRequestNeedsNoSession is the core stateless guarantee: a client on
// 2026-07-28 sends no Mcp-Session-Id and performs no initialize handshake, and
// must still get a full tools/list.
func TestModernRequestNeedsNoSession(t *testing.T) {
	handler := newTestServer(t).Handler()

	resp, env := mcpModern(t, handler, 1, "tools/list", `{}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if env.Error != nil {
		t.Fatalf("stateless tools/list errored: %+v", env.Error)
	}

	if len(env.Result) == 0 {
		t.Fatal("stateless tools/list returned no result")
	}

	// A stateless client has nowhere to put a session ID and will not echo one
	// back; minting one per request would grow the session table unboundedly.
	if got := resp.Header.Get("Mcp-Session-Id"); got != "" {
		t.Errorf("server minted a session id (%q) for a stateless client", got)
	}
}

// TestIdentityDoesNotComeFromSession guards the security property. ACL
// decisions must derive from the request's bearer token every time; a
// session-cached identity would let a revoked token keep working.
func TestIdentityDoesNotComeFromSession(t *testing.T) {
	for _, name := range []string{"tools.go", "resources.go", "acl.go"} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		if strings.Contains(string(src), "ClientSessionFromContext") {
			t.Errorf("%s reads session state; identity must come from the request context", name)
		}
	}
}

// TestSessionsRemainAvailableForLegacyClients pins the other half: dropping
// session support would break every client still on the initialize handshake.
func TestSessionsRemainAvailableForLegacyClients(t *testing.T) {
	handler := newTestServer(t).Handler()

	if sessionID := initSession(t, handler, ""); sessionID == "" {
		t.Fatal("legacy initialize did not yield a session")
	}
}
