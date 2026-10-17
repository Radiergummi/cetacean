package mcp

import (
	"net/http"
	"testing"
)

// mcp-go answers a URI no template matches with its not-found code, and any
// error a matched template's handler returns as -32603. A stale or mistyped ID
// under a real template is not found all the same, and answers the same code.
func TestResourceReadOfAnUnknownIDUnderATemplateIsNotFound(t *testing.T) {
	handler := newTestServer(t).Handler()

	t.Run("2026-07-28", func(t *testing.T) {
		_, env := sendMCP(t, handler,
			modernRequest(t, 1, "resources/read", `{"uri":"cetacean://services/no-such-service"}`))

		if env.Error == nil || env.Error.Code != -32602 {
			t.Fatalf("error = %+v, want -32602", env.Error)
		}
	})

	t.Run("2025-11-25", func(t *testing.T) {
		status, env := postRaw(
			t,
			handler,
			`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"cetacean://services/no-such-service"}}`,
			legacyHeaders,
		)

		if status != http.StatusOK || env.Error == nil || env.Error.Code != -32002 {
			t.Fatalf("status %d, error = %+v, want -32002", status, env.Error)
		}
	})
}
