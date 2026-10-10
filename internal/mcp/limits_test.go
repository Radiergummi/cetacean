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

// The body cap must not answer for the origin or bearer check, which owe
// 403 and 401 before anything reads the body.
func TestGuardsAnswerBeforeTheBodyCap(t *testing.T) {
	oversized := func() *http.Request {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"pad":"` +
			strings.Repeat("a", maxRequestBytes) + `"}}`
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		return req
	}

	t.Run("forged origin", func(t *testing.T) {
		srv := newToolTestServer(
			t,
			cache.New(nil),
			&fakeWriteClient{},
			config.OpsReadOnly,
			func(o *Options) { o.AllowedOrigins = []string{"https://good.example"} },
		)
		req := oversized()
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("no bearer", func(t *testing.T) {
		cfg := config.DefaultMCPConfig()
		cfg.Enabled = true
		srv, err := New(cache.New(nil), Options{
			Config:   cfg,
			OAuth:    oauthServerFor([]byte("test-secret-32-bytes-long-padding")),
			Resource: testResource,
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, oversized())

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

func TestHandlerCapsListenStreams(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsReadOnly)
	for range maxListenStreams {
		srv.listens <- struct{}{}
	}

	rec := httptest.NewRecorder()
	srv.Handler().
		ServeHTTP(rec, modernRequest(t, 1, "subscriptions/listen", `{"notifications":{}}`))

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("listen past the cap: status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("listen past the cap: no Retry-After")
	}

	// A full cap must not answer for the origin guard, which owes a 403.
	forged := modernRequest(t, 3, "subscriptions/listen", `{"notifications":{}}`)
	forged.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, forged)

	if rec.Code != http.StatusForbidden {
		t.Errorf("forged origin while listens are full: status = %d, want 403", rec.Code)
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
