package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

func TestBasePathFromContext(t *testing.T) {
	t.Run("empty context returns empty string", func(t *testing.T) {
		got := BasePathFromContext(context.Background())
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("returns stored value", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), basePathKey, "/cetacean")
		got := BasePathFromContext(ctx)
		if got != "/cetacean" {
			t.Errorf("expected %q, got %q", "/cetacean", got)
		}
	})
}

func TestAbsPath(t *testing.T) {
	cases := []struct {
		basePath string
		path     string
		want     string
	}{
		{"", "/nodes", "/nodes"},
		{"", "/", "/"},
		{"/cetacean", "/nodes", "/cetacean/nodes"},
		{"/cetacean", "/", "/cetacean/"},
		{"/prefix/sub", "/services/abc", "/prefix/sub/services/abc"},
	}

	for _, tc := range cases {
		ctx := context.Background()
		if tc.basePath != "" {
			ctx = context.WithValue(ctx, basePathKey, tc.basePath)
		}
		got := absPath(ctx, tc.path)
		if got != tc.want {
			t.Errorf("absPath(ctx{%q}, %q) = %q, want %q", tc.basePath, tc.path, got, tc.want)
		}
	}
}

func TestBasePathMiddleware_Strips(t *testing.T) {
	var capturedPath string
	var capturedBasePath string

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedBasePath = BasePathFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := basePathMiddleware("/cetacean", inner)

	req := httptest.NewRequest(http.MethodGet, "/cetacean/nodes", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if capturedPath != "/nodes" {
		t.Errorf("inner saw path %q, want %q", capturedPath, "/nodes")
	}
	if capturedBasePath != "/cetacean" {
		t.Errorf("inner context base path = %q, want %q", capturedBasePath, "/cetacean")
	}
}

func TestBasePathMiddleware_Root(t *testing.T) {
	cases := []struct {
		url string
	}{
		{"/cetacean"},
		{"/cetacean/"},
	}

	for _, tc := range cases {
		var capturedPath string

		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		})

		handler := basePathMiddleware("/cetacean", inner)

		req := httptest.NewRequest(http.MethodGet, tc.url, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("url=%q: expected 200, got %d", tc.url, rec.Code)
		}
		if capturedPath != "/" {
			t.Errorf("url=%q: inner saw path %q, want %q", tc.url, capturedPath, "/")
		}
	}
}

func TestBasePathMiddleware_Mismatch(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := basePathMiddleware("/cetacean", inner)

	for _, path := range []string{"/other/path", "/cetaceannodes"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("path=%q: expected 404, got %d", path, rec.Code)
		}
	}
}

func TestBasePathMiddleware_TrailingSlashRedirect(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := basePathMiddleware("/cetacean", inner)

	req := httptest.NewRequest(http.MethodGet, "/cetacean/nodes/?sort=name", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	want := "/cetacean/nodes?sort=name"
	if location != want {
		t.Errorf("Location = %q, want %q", location, want)
	}
}

func TestBasePathMiddleware_Empty(t *testing.T) {
	var capturedPath string

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	handler := basePathMiddleware("", inner)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if capturedPath != "/nodes" {
		t.Errorf("path = %q, want %q", capturedPath, "/nodes")
	}
}

func TestAbsURLPrefersPublicURL(t *testing.T) {
	called := false
	handler := publicURLMiddleware(
		"https://cetacean.example.com",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			if got := absURL(r, "/services"); got != "https://cetacean.example.com/services" {
				t.Errorf("absURL = %q, want %q", got, "https://cetacean.example.com/services")
			}
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	req.Host = "internal:9000"
	req.Header.Set("X-Forwarded-Host", "attacker.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Fatal("handler was never invoked")
	}
}

func TestAbsURLPrefersPublicURLWithBasePath(t *testing.T) {
	called := false
	handler := publicURLMiddleware(
		"https://cetacean.example.com",
		basePathMiddleware(
			"/cetacean",
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				want := "https://cetacean.example.com/cetacean/services"
				if got := absURL(r, "/services"); got != want {
					t.Errorf("absURL = %q, want %q", got, want)
				}
			}),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/cetacean/services", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler was never invoked")
	}
}

// TestAbsURLOriginIsProxySupplied covers what absURL may believe about the
// origin a client reached. Forwarded and X-Forwarded-* are request headers
// like any other: anyone can send them, and whatever they say ends up in URLs
// Cetacean publishes in feeds and discovery documents. They are read only when
// the peer the connection actually arrived from is a configured trusted proxy
// — auth.Peer, recorded once at the edge by realIP — and only when the value
// itself can stand where it is going.
func TestAbsURLOriginIsProxySupplied(t *testing.T) {
	const (
		trusted   = "203.0.113.7"
		publicURL = "https://cetacean.example.com"
	)

	tests := []struct {
		name      string
		publicURL string
		peer      *auth.Peer
		headers   map[string]string
		want      string
	}{
		{
			name:    "no verdict recorded believes nothing",
			headers: map[string]string{"X-Forwarded-Host": "proxy.example.com"},
			want:    "http://internal:9000/services",
		},
		{
			name: "untrusted peer believes nothing",
			peer: &auth.Peer{Addr: netip.MustParseAddr(trusted), Trusted: false},
			headers: map[string]string{
				"X-Forwarded-Host":  "proxy.example.com",
				"X-Forwarded-Proto": "https",
			},
			want: "http://internal:9000/services",
		},
		{
			name: "trusted peer is read from X-Forwarded-*",
			peer: &auth.Peer{Addr: netip.MustParseAddr(trusted), Trusted: true},
			headers: map[string]string{
				"X-Forwarded-Host":  "proxy.example.com",
				"X-Forwarded-Proto": "https",
			},
			want: "https://proxy.example.com/services",
		},
		{
			name: "trusted peer is read from Forwarded",
			peer: &auth.Peer{Addr: netip.MustParseAddr(trusted), Trusted: true},
			headers: map[string]string{
				"Forwarded": `for=192.0.2.9;host=proxy.example.com;proto=https`,
			},
			want: "https://proxy.example.com/services",
		},
		{
			name: "Forwarded wins over the pair it standardizes",
			peer: &auth.Peer{Addr: netip.MustParseAddr(trusted), Trusted: true},
			headers: map[string]string{
				"Forwarded":         `host=rfc7239.example.com;proto=https`,
				"X-Forwarded-Host":  "defacto.example.com",
				"X-Forwarded-Proto": "http",
			},
			want: "https://rfc7239.example.com/services",
		},
		{
			name: "the leftmost element saw the client",
			peer: &auth.Peer{Addr: netip.MustParseAddr(trusted), Trusted: true},
			headers: map[string]string{
				"Forwarded": `host=edge.example.com;proto=https, host=inner:9000;proto=http`,
			},
			want: "https://edge.example.com/services",
		},
		{
			// nginx's proxy_set_header X-Forwarded-Host $http_host passes the
			// client's own Host through: a trusted proxy is not a promise
			// that the value it forwarded was ever checked.
			name:    "a host that would rewrite the link is refused",
			peer:    &auth.Peer{Addr: netip.MustParseAddr(trusted), Trusted: true},
			headers: map[string]string{"X-Forwarded-Host": "evil.example.com/attacker#"},
			want:    "http://internal:9000/services",
		},
		{
			name:    "a scheme that is not http(s) is refused",
			peer:    &auth.Peer{Addr: netip.MustParseAddr(trusted), Trusted: true},
			headers: map[string]string{"X-Forwarded-Proto": "javascript"},
			want:    "http://internal:9000/services",
		},
		{
			name:      "server.public_url outranks any of it",
			publicURL: publicURL,
			peer:      &auth.Peer{Addr: netip.MustParseAddr(trusted), Trusted: true},
			headers: map[string]string{
				"X-Forwarded-Host":  "proxy.example.com",
				"X-Forwarded-Proto": "http",
			},
			want: publicURL + "/services",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool

			handler := publicURLMiddleware(
				tt.publicURL,
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true

					if got := absURL(r, "/services"); got != tt.want {
						t.Errorf("absURL = %q, want %q", got, tt.want)
					}
				}),
			)

			req := httptest.NewRequest(http.MethodGet, "/services", nil)
			req.Host = "internal:9000"

			for name, value := range tt.headers {
				req.Header.Set(name, value)
			}

			if tt.peer != nil {
				req = req.WithContext(auth.ContextWithPeer(req.Context(), *tt.peer))
			}

			handler.ServeHTTP(httptest.NewRecorder(), req)

			if !called {
				t.Fatal("handler was never invoked")
			}
		})
	}
}
