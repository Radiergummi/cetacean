package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
