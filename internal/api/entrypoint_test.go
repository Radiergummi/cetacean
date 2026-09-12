package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// fetchEntrypoint reads the entry point as JSON and decodes it.
func fetchEntrypoint(t *testing.T, router http.Handler, path string) map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; body=%s", path, rec.Code, rec.Body.String())
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("parse entry point: %v", err)
	}

	return doc
}

// resourceLinks reads the entry point's resources as term -> @id.
func resourceLinks(t *testing.T, doc map[string]any) map[string]string {
	t.Helper()

	resources, ok := doc["resources"].(map[string]any)
	if !ok {
		t.Fatalf("resources = %#v, want an object", doc["resources"])
	}

	links := make(map[string]string, len(resources))

	for term, value := range resources {
		entry, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("resources[%q] = %#v, want an object", term, value)
		}

		id, ok := entry["@id"].(string)
		if !ok {
			t.Fatalf("resources[%q] has no @id; a term under @vocab is a literal, not a link", term)
		}

		links[term] = id
	}

	return links
}

// The document is a client's only map of the API, so a term naming nothing is
// worse than no term at all.
func TestEntrypointNamesOnlyRegisteredRoutes(t *testing.T) {
	patterns := routerPatterns(t)

	for _, name := range entrypointResources {
		if !slices.Contains(patterns, "GET /"+name) {
			t.Errorf("the entry point names %q, which the router does not register", name)
		}
	}
}

func TestEntrypointNamesEveryResourceAsALink(t *testing.T) {
	doc := fetchEntrypoint(t, newTestRouterWithConfig(t, nil), "/")

	if got := doc["@id"]; got != "/" {
		t.Errorf("@id = %v, want /", got)
	}

	if got := doc["@type"]; got != "EntryPoint" {
		t.Errorf("@type = %v, want EntryPoint", got)
	}

	links := resourceLinks(t, doc)

	if len(links) != len(entrypointResources) {
		t.Errorf("names %d resources, want %d", len(links), len(entrypointResources))
	}

	for _, name := range entrypointResources {
		if got := links[name]; got != "/"+name {
			t.Errorf("resources[%q] = %q, want /%s", name, got, name)
		}
	}
}

// The root is the one address a browser and a script both start from, so it
// answers each in its own format — and a suffix outranks the Accept header.
func TestRootNegotiatesBetweenDashboardAndEntrypoint(t *testing.T) {
	router := newTestRouterWithConfig(t, nil)

	for _, tt := range []struct{ name, path, accept, want string }{
		{"a browser", "/", "text/html", "text/html"},
		{"a script", "/", "application/json", "application/json"},
		{"no preference", "/", "*/*", "application/json"},
		{"an absent Accept", "/", "", "application/json"},
		{".html over a JSON Accept", "/.html", "application/json", "text/html"},
		{".json over an HTML Accept", "/.json", "text/html", "application/json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200; body=%s", tt.path, rec.Code, rec.Body.String())
			}

			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tt.want) {
				t.Errorf("GET %s content-type = %q, want %s", tt.path, ct, tt.want)
			}
		})
	}
}

// The root has two representations, so unlike the catch-all below it there is
// something for a 406 to be about.
func TestRootRefusesATypeItCannotProduce(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "image/png")

	rec := httptest.NewRecorder()
	newTestRouterWithConfig(t, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotAcceptable {
		t.Errorf("status=%d, want 406; body=%s", rec.Code, rec.Body.String())
	}
}

// negotiate strips the suffix before routing, so one route covers all three;
// the suffix goes back on the target, which names the representation.
func TestIndexRedirectsToTheRootKeepingTheSuffix(t *testing.T) {
	router := newTestRouterWithConfig(t, nil)

	for _, tt := range []struct{ path, want string }{
		{"/index", "/"},
		{"/index.html", "/.html"},
		{"/index.json", "/.json"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusMovedPermanently {
				t.Fatalf("GET %s = %d, want 301", tt.path, rec.Code)
			}

			if got := rec.Header().Get("Location"); got != tt.want {
				t.Errorf("GET %s Location = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// Under CETACEAN_BASE_PATH every link must carry the prefix, or it addresses a
// path the deployment does not serve.
func TestEntrypointIsAbsoluteUnderABasePath(t *testing.T) {
	router := newBasePathTestRouter(t, "/cetacean")

	doc := fetchEntrypoint(t, router, "/cetacean/")

	if got := doc["@id"]; got != "/cetacean/" {
		t.Errorf("@id = %v, want /cetacean/", got)
	}

	for term, id := range resourceLinks(t, doc) {
		if !strings.HasPrefix(id, "/cetacean/") {
			t.Errorf("resources[%q] = %q, which does not carry the base path", term, id)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/cetacean/index.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Location"); got != "/cetacean/.json" {
		t.Errorf("Location = %q, want /cetacean/.json", got)
	}
}

// Fails if the resources serialize in map order: Go randomizes it, so no
// conditional GET would ever match.
func TestEntrypointETagIsStable(t *testing.T) {
	router := newTestRouterWithConfig(t, nil)

	var first string

	for i := range 8 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", "application/json")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		etag := rec.Header().Get("ETag")
		if etag == "" {
			t.Fatal("the entry point carries no ETag")
		}

		if i == 0 {
			first = etag

			continue
		}

		if etag != first {
			t.Fatalf("ETag changed between renders: %q then %q", first, etag)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("If-None-Match", first)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Errorf("conditional GET = %d, want 304", rec.Code)
	}
}
