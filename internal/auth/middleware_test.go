package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// failProvider always returns an error.
type failProvider struct{}

func (p *failProvider) Authenticate(_ http.ResponseWriter, _ *http.Request) (*Identity, error) {
	return nil, errors.New("access denied")
}

func (p *failProvider) RegisterRoutes(_ *http.ServeMux) {}

// redirectProvider writes a redirect and returns (nil, nil).
type redirectProvider struct{}

func (p *redirectProvider) Authenticate(w http.ResponseWriter, r *http.Request) (*Identity, error) {
	http.Redirect(w, r, "/auth/login", http.StatusFound)
	return nil, nil
}

func (p *redirectProvider) RegisterRoutes(_ *http.ServeMux) {}

func TestMiddleware_NoneProvider_InjectsIdentity(t *testing.T) {
	var gotIdentity *Identity
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIdentity = IdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(&NoneProvider{})(inner)
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotIdentity == nil {
		t.Fatal("expected identity in context, got nil")
	}
	if gotIdentity.Subject != "anonymous" {
		t.Errorf("Subject = %q, want %q", gotIdentity.Subject, "anonymous")
	}
}

func TestMiddleware_ExemptRoutes(t *testing.T) {
	cases := []string{
		"/-/health",
		"/-/ready",
		"/-/metrics/status",
		"/api",
		"/api/context.jsonld",
		"/api/scalar.js",
		"/assets/index.js",
		"/auth/callback",
	}

	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			called := false
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				// Identity should NOT be in context for exempt routes.
				if id := IdentityFromContext(r.Context()); id != nil {
					t.Errorf("expected no identity for exempt path %s, got %+v", path, id)
				}
				w.WriteHeader(http.StatusOK)
			})

			handler := Middleware(&failProvider{})(inner)
			r := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if !called {
				t.Errorf("inner handler not called for exempt path %s", path)
			}
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d for exempt path %s", w.Code, http.StatusOK, path)
			}
		})
	}
}

func TestMiddleware_AuthError_Returns401(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := Middleware(&failProvider{})(inner)
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if called {
		t.Error("inner handler should not be called on auth error")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// bearerProvider returns an AuthError with WWW-Authenticate header.
type bearerProvider struct{}

func (p *bearerProvider) Authenticate(_ http.ResponseWriter, _ *http.Request) (*Identity, error) {
	return nil, &AuthError{Msg: "authentication required", WWWAuthenticate: "Bearer"}
}
func (p *bearerProvider) RegisterRoutes(_ *http.ServeMux) {}

func TestMiddleware_AuthError_SetsWWWAuthenticate(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	})

	handler := Middleware(&bearerProvider{})(inner)
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want %q", got, "Bearer")
	}
}

func TestMiddleware_PlainError_NoWWWAuthenticate(t *testing.T) {
	handler := Middleware(&failProvider{})(http.NotFoundHandler())
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("WWW-Authenticate"); got != "" {
		t.Errorf("WWW-Authenticate = %q, want empty", got)
	}
}

func TestMiddleware_RedirectProvider_InnerNotCalled(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := Middleware(&redirectProvider{})(inner)
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if called {
		t.Error("inner handler should not be called when provider handles response")
	}
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
}
