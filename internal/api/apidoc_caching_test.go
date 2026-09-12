package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// apiDocRoutes names the two endpoints and a marker their body must contain,
// so a test that silently received the SPA or a 404 fails rather than passing
// on an empty comparison.
var apiDocRoutes = []struct {
	name     string
	path     string
	contains string
}{
	{name: "openapi document", path: "/api", contains: "openapi"},
	{name: "scalar bundle", path: "/api/scalar.js", contains: "scalar"},
}

func newAPIDocRouter(t testing.TB) http.Handler {
	t.Helper()

	var spec strings.Builder
	spec.WriteString("openapi: '3.1.0'\ninfo:\n  title: Cetacean\n  version: '1'\npaths:\n")
	for i := range 200 {
		fmt.Fprintf(&spec, "  /padding/%d:\n    get:\n      summary: padding\n", i)
	}

	scalarJS := "/* scalar bundle */ globalThis.scalar = {};\n" +
		strings.Repeat("// padding to exceed the compression threshold\n", 40)

	return newTestRouterWithConfig(t, []routerOption{withAPIDocs(
		[]byte(spec.String()),
		[]byte(scalarJS),
	)})
}

// TestAPIDocsAreCompressed fails while either endpoint bypasses the write
// helpers: a bare w.Write emits no Content-Encoding whatever the client asks
// for.
func TestAPIDocsAreCompressed(t *testing.T) {
	router := newAPIDocRouter(t)

	for _, route := range apiDocRoutes {
		t.Run(route.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", route.path, nil)
			req.Header.Set("Accept-Encoding", "zstd")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Encoding"); got != "zstd" {
				t.Fatalf("Content-Encoding = %q, want zstd", got)
			}

			plain := decodeZstd(t, rec.Body.Bytes())
			if !strings.Contains(string(plain), route.contains) {
				t.Errorf("decompressed body does not contain %q", route.contains)
			}
		})
	}
}

// TestAPIDocsRevalidate fails while either endpoint answers without a
// validator: both are cached for an hour or a day, so a client that comes
// back has no way to learn the document is unchanged short of re-downloading
// it.
func TestAPIDocsRevalidate(t *testing.T) {
	router := newAPIDocRouter(t)

	for _, route := range apiDocRoutes {
		t.Run(route.name, func(t *testing.T) {
			first := httptest.NewRequest("GET", route.path, nil)
			firstRec := httptest.NewRecorder()
			router.ServeHTTP(firstRec, first)

			etag := firstRec.Header().Get("ETag")
			if etag == "" {
				t.Fatal("no ETag on the first response")
			}

			second := httptest.NewRequest("GET", route.path, nil)
			second.Header.Set("If-None-Match", etag)
			secondRec := httptest.NewRecorder()
			router.ServeHTTP(secondRec, second)

			if secondRec.Code != http.StatusNotModified {
				t.Errorf("status = %d, want 304", secondRec.Code)
			}
			if secondRec.Body.Len() != 0 {
				t.Errorf("304 carried a %d-byte body", secondRec.Body.Len())
			}
		})
	}
}

// TestAPIDocsKeepTheirCacheControl guards the one header the shared helper
// would otherwise supply itself: it defaults to no-cache, and these two
// endpoints deliberately set a long max-age instead.
func TestAPIDocsKeepTheirCacheControl(t *testing.T) {
	router := newAPIDocRouter(t)

	for _, route := range apiDocRoutes {
		t.Run(route.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", route.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "max-age") {
				t.Errorf("Cache-Control = %q, want a max-age", got)
			}
		})
	}
}
