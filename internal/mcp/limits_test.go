package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// mcp-go reads a body whole, so without a cap one request can take the host's
// memory.
func TestHandlerRefusesAnOversizedBody(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsReadOnly)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find","arguments":{"query":"` +
		strings.Repeat(
			"a",
			maxRequestBytes,
		) + `"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

func TestHandlerCapsListenStreams(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsReadOnly)
	for range maxListenStreams {
		srv.listens <- struct{}{}
	}

	rec := httptest.NewRecorder()
	srv.Handler().
		ServeHTTP(rec, modernRequest(t, 1, "subscriptions/listen", `{"notifications":{}}`))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("listen past the cap: status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("listen past the cap: no Retry-After")
	}

	// Only listen streams count against the cap.
	resp, env := sendMCP(t, srv.Handler(), modernRequest(t, 2, "tools/list", `{}`))
	if resp.StatusCode != http.StatusOK || env.Error != nil {
		t.Errorf(
			"tools/list while listens are full: status = %d, error = %+v",
			resp.StatusCode,
			env.Error,
		)
	}
}
