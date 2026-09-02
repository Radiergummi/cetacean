package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORS_Disabled(t *testing.T) {
	handler := cors(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/nodes", nil)
	r.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no ACAO header when disabled, got %q", got)
	}
}

func TestCORS_AllowedOrigin(t *testing.T) {
	cfg := &CORSConfig{AllowedOrigins: []string{"https://example.com"}}
	handler := cors(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/nodes", nil)
	r.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("ACAO = %q, want %q", got, "https://example.com")
	}
	if got := w.Header().Get("Access-Control-Expose-Headers"); got == "" {
		t.Error("expected Expose-Headers to be set")
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want %q", got, "Origin")
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	cfg := &CORSConfig{AllowedOrigins: []string{"https://example.com"}}
	handler := cors(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/nodes", nil)
	r.Header.Set("Origin", "https://evil.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no ACAO for disallowed origin, got %q", got)
	}
}

func TestCORS_Wildcard(t *testing.T) {
	cfg := &CORSConfig{AllowedOrigins: []string{"*"}}
	handler := cors(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/nodes", nil)
	r.Header.Set("Origin", "https://anything.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://anything.example.com" {
		t.Errorf("ACAO = %q, want reflected origin", got)
	}
}

func TestCORS_Preflight(t *testing.T) {
	cfg := &CORSConfig{AllowedOrigins: []string{"https://example.com"}}
	handler := cors(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("preflight should not reach the next handler")
	}))

	r := httptest.NewRequest("OPTIONS", "/services", nil)
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Access-Control-Request-Method", "PATCH")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("expected Allow-Methods on preflight")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("expected Allow-Headers on preflight")
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("Max-Age = %q, want %q", got, "86400")
	}
}

func TestCORS_NoOriginHeader(t *testing.T) {
	cfg := &CORSConfig{AllowedOrigins: []string{"*"}}
	handler := cors(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/nodes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no ACAO without Origin header, got %q", got)
	}
}

// TestCORSAllowsMCPRequestHeaders — a browser-based MCP host cannot send the
// headers 2026-07-28 requires on every POST unless preflight allows them.
// Mcp-Session-Id covers legacy clients, which carry it on every request after
// the initialize handshake.
func TestCORSAllowsMCPRequestHeaders(t *testing.T) {
	handler := cors(&CORSConfig{AllowedOrigins: []string{"https://host.example"}})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	)

	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	req.Header.Set("Origin", "https://host.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "mcp-method,mcp-name,mcp-protocol-version")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	allowed := strings.ToLower(w.Header().Get("Access-Control-Allow-Headers"))
	for _, header := range []string{"mcp-method", "mcp-name", "mcp-protocol-version", "mcp-session-id"} {
		if !strings.Contains(allowed, header) {
			t.Errorf("preflight does not allow %q (got %q)", header, allowed)
		}
	}
}

// TestCORSExposesSessionHeader — a legacy browser client reads its session ID
// off the initialize response, which it cannot do unless the header is exposed.
func TestCORSExposesSessionHeader(t *testing.T) {
	handler := cors(&CORSConfig{AllowedOrigins: []string{"https://host.example"}})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Origin", "https://host.example")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	exposed := strings.ToLower(w.Header().Get("Access-Control-Expose-Headers"))
	if !strings.Contains(exposed, "mcp-session-id") {
		t.Errorf("Mcp-Session-Id not exposed to cross-origin scripts (got %q)", exposed)
	}
}
