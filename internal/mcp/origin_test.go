package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// TestOriginGuard verifies the MCP handler rejects forged Origin headers with
// 403 (DNS-rebinding defense required by the MCP Streamable HTTP transport)
// while letting allowed origins, empty origins, and wildcard config through.
func TestOriginGuard(t *testing.T) {
	newHandler := func(allowed []string) http.Handler {
		srv := newToolTestServer(
			t,
			cache.New(nil),
			&fakeWriteClient{},
			config.OpsReadOnly,
			func(o *Options) { o.AllowedOrigins = allowed },
		)
		return srv.Handler()
	}

	post := func(h http.Handler, origin string) int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	t.Run("forged origin is rejected", func(t *testing.T) {
		h := newHandler([]string{"https://good.example"})
		if got := post(h, "https://evil.example"); got != http.StatusForbidden {
			t.Errorf("forged origin: status = %d, want %d", got, http.StatusForbidden)
		}
	})

	t.Run("allowed origin passes", func(t *testing.T) {
		h := newHandler([]string{"https://good.example"})
		if got := post(h, "https://good.example"); got == http.StatusForbidden {
			t.Errorf("allowed origin: status = %d, want not 403", got)
		}
	})

	t.Run("missing origin passes", func(t *testing.T) {
		h := newHandler([]string{"https://good.example"})
		if got := post(h, ""); got == http.StatusForbidden {
			t.Errorf("missing origin: status = %d, want not 403", got)
		}
	})

	t.Run("wildcard allows any origin", func(t *testing.T) {
		h := newHandler([]string{"*"})
		if got := post(h, "https://anything.example"); got == http.StatusForbidden {
			t.Errorf("wildcard origin: status = %d, want not 403", got)
		}
	})
}
