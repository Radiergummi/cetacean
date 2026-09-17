package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"

	"github.com/radiergummi/cetacean/internal/spec"
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
	spec.Satisfies(t, "oauth/rfc9700/cors-may-be-supported-at-the-other-endpoints")

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

// RFC 9700 §2.6 forbids CORS at the authorization endpoint: the client reaches
// it by redirecting the user agent, never by fetch, so reflecting an origin
// there would only let a page on it read the consent form and the CSRF token
// in it. The request still reaches the handler — it is the headers that stay
// off.
func TestCORSIsNotAnsweredForTheAuthorizationEndpoint(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc9700/no-cors-at-the-authorization-endpoint")

	cfg := &CORSConfig{AllowedOrigins: []string{"https://example.com"}}

	var reached bool
	handler := cors(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			reached = false

			r := httptest.NewRequest(method, "/oauth/authorize?response_type=code", nil)
			r.Header.Set("Origin", "https://example.com")
			if method == http.MethodOptions {
				r.Header.Set("Access-Control-Request-Method", "POST")
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("ACAO = %q, want none at the authorization endpoint", got)
			}
			if got := w.Header().Get("Access-Control-Allow-Methods"); got != "" {
				t.Errorf("Allow-Methods = %q, want none: there is no preflight to answer", got)
			}
			if !reached {
				t.Error("the request did not reach the handler")
			}
		})
	}
}

// The same rule through the assembled router, which is the only place the
// spellings the endpoint answers to are all reachable: a base path, and an
// extension suffix negotiate strips on the way in.
func TestTheAuthorizationEndpointReflectsNoOriginAtAnySpelling(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc9700/no-cors-at-the-authorization-endpoint")

	for _, basePath := range []string{"", "/a/b"} {
		for _, suffix := range []string{"", ".html", ".json"} {
			t.Run(basePath+"/oauth/authorize"+suffix, func(t *testing.T) {
				router := newTestRouterWithConfig(
					t,
					[]routerOption{
						withBasePath(basePath),
						withOAuthRoutes(basePath),
						withCORS("https://example.com"),
					},
					withCache(cache.New(nil)),
				)

				r := httptest.NewRequest(
					http.MethodGet,
					basePath+"/oauth/authorize"+suffix+"?response_type=code",
					nil,
				)
				r.Header.Set("Origin", "https://example.com")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)

				if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
					t.Errorf("ACAO = %q, want none at the authorization endpoint", got)
				}
			})
		}
	}
}
