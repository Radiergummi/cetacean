package mcp

import (
	"context"
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
