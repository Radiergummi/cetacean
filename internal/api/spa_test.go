package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAServesPrecompressedVariants(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":            {Data: []byte("<html><head></head></html>")},
		"assets/app-abc.js":     {Data: bytes.Repeat([]byte("x"), 4096)},
		"assets/app-abc.js.zst": {Data: []byte("fake-zstd")},
		"assets-manifest.json": {Data: []byte(`{
			"assets/app-abc.js": {"size": 4096, "etag": "aaaa",
				"variants": {"zstd": {"size": 9, "etag": "bbbb"}}}
		}`)},
	}
	handler := NewSPAHandler(fsys, "")

	req := httptest.NewRequest("GET", "/assets/app-abc.js", nil)
	req.Header.Set("Accept-Encoding", "zstd")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "zstd" {
		t.Errorf("Content-Encoding = %q, want zstd", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Errorf("Content-Type = %q, want JavaScript from the original extension", got)
	}
	if got := rec.Body.String(); got != "fake-zstd" {
		t.Errorf("body = %q, want the .zst variant", got)
	}
	// The manifest stores the bare hex digest; the ETag header must carry it
	// quoted, per RFC 9110, or the validator never matches.
	if got := rec.Header().Get("ETag"); got != `"bbbb"` {
		t.Errorf("ETag = %q, want %q", got, `"bbbb"`)
	}
}

func TestSPAServesIdentityETagQuoted(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":        {Data: []byte("<html><head></head></html>")},
		"assets/app-abc.js": {Data: []byte("content")},
		"assets-manifest.json": {Data: []byte(
			`{"assets/app-abc.js":{"size":7,"etag":"aaaa","variants":{}}}`,
		)},
	}
	handler := NewSPAHandler(fsys, "")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/assets/app-abc.js", nil))

	if got := rec.Header().Get("ETag"); got != `"aaaa"` {
		t.Errorf("ETag = %q, want %q", got, `"aaaa"`)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want none for identity", got)
	}
}

func TestSPACacheHeaders(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":        {Data: []byte("<html><head></head></html>")},
		"assets/app-abc.js": {Data: []byte("content")},
		"assets-manifest.json": {Data: []byte(
			`{"assets/app-abc.js":{"size":7,"etag":"aaaa","variants":{}}}`,
		)},
	}
	handler := NewSPAHandler(fsys, "")

	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest("GET", "/assets/app-abc.js", nil))
	if got := asset.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("asset Cache-Control = %q, want immutable", got)
	}

	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest("GET", "/", nil))
	if got := index.Header().Get("Cache-Control"); !strings.Contains(got, "no-cache") {
		t.Errorf("index Cache-Control = %q, want no-cache", got)
	}
}

// TestSPADirectoryPathIsNotAnAsset guards the seam between the existence check
// and serveAsset: a directory opens, but cannot be seeked, so probing /assets
// used to answer 500 instead of the SPA shell that /assets/ already answers.
func TestSPADirectoryPathIsNotAnAsset(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":        {Data: []byte("<html><head></head></html>")},
		"assets/app-abc.js": {Data: []byte("content")},
	}
	handler := NewSPAHandler(fsys, "")

	for _, path := range []string{"/assets", "/assets/"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

			if rec.Code != http.StatusOK {
				t.Errorf("GET %s = %d, want %d", path, rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
				t.Errorf("GET %s Content-Type = %q, want the SPA shell", path, got)
			}
		})
	}
}

// TestSPAServesWebManifestAsJSON pins the manifest's media type. Sniffing
// reports text/plain for JSON, which browsers must reject — silently, so this
// stands in for an install prompt the suite cannot see.
func TestSPAServesWebManifestAsJSON(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":           {Data: []byte("<html><head></head></html>")},
		"manifest.webmanifest": {Data: []byte(`{"name":"Cetacean","start_url":"./"}`)},
	}
	handler := NewSPAHandler(fsys, "")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/manifest.webmanifest", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	got, _, _ := strings.Cut(rec.Header().Get("Content-Type"), ";")
	if strings.TrimSpace(got) != "application/manifest+json" {
		t.Errorf("Content-Type = %q, want application/manifest+json", got)
	}
}

// webManifestPath is read from source, so these tests need no build output.
const webManifestPath = "../../frontend/public/manifest.webmanifest"

type webManifestIcon struct {
	Src   string `json:"src"`
	Sizes string `json:"sizes"`
}

// webManifest is the subset these tests make claims about.
type webManifest struct {
	StartURL   string            `json:"start_url"`
	Scope      string            `json:"scope"`
	ThemeColor string            `json:"theme_color"`
	Icons      []webManifestIcon `json:"icons"`
}

func readWebManifest(t *testing.T) webManifest {
	t.Helper()

	raw, err := os.ReadFile(webManifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	var manifest webManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}

	return manifest
}

// TestUnreadySharesTheFrontendButNotTheCluster holds the readiness gate to the
// endpoints that read cluster state. The shell and everything it pulls — the
// web manifest, the icons — come off the embedded filesystem, so an unreachable
// Docker daemon must not turn them into problem documents; the browser asks for
// them with */*, which negotiates to JSON like any cluster read.
func TestUnreadySharesTheFrontendButNotTheCluster(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":           {Data: []byte("<html><head></head></html>")},
		"manifest.webmanifest": {Data: []byte(`{"name":"Cetacean"}`)},
		"favicon-32x32.png":    {Data: []byte("favicon")},
		"apple-touch-icon.png": {Data: []byte("touch-icon")},
	}

	// Never closed: the daemon is unreachable for the whole test.
	router := newTestRouterWithConfig(
		t,
		[]routerOption{withSPAFiles(fsys)},
		withReady(make(chan struct{})),
	)

	get := func(t *testing.T, path string) *httptest.ResponseRecorder {
		t.Helper()

		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept", "*/*")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		return rec
	}

	for _, file := range []string{
		"/",
		"/manifest.webmanifest",
		"/favicon-32x32.png",
		"/apple-touch-icon.png",
	} {
		t.Run(file, func(t *testing.T) {
			rec := get(t, file)

			if rec.Code != http.StatusOK {
				t.Errorf("GET %s = %d, want %d", file, rec.Code, http.StatusOK)
			}
			got := rec.Header().Get("Content-Type")
			if strings.HasPrefix(got, "application/problem") {
				t.Errorf("GET %s Content-Type = %q, want the file", file, got)
			}
		})
	}

	// The manifest is served as itself, not as the index.html the SPA falls
	// back to for a client-side route.
	if got := get(t, "/manifest.webmanifest").Body.String(); got != `{"name":"Cetacean"}` {
		t.Errorf("manifest body = %q, want the file from the embedded filesystem", got)
	}

	// Registered routes that answer from the request rather than the cache. A
	// client probing a server it cannot reach is when the discovery documents
	// are worth most, and identity says who you are, not what the cluster is.
	for _, path := range []string{apiCatalogPath, openSearchPath, profilePath} {
		t.Run(path, func(t *testing.T) {
			rec := get(t, path)

			if rec.Code != http.StatusOK {
				t.Errorf("GET %s = %d, want %d", path, rec.Code, http.StatusOK)
			}
		})
	}

	for _, resource := range []string{"/nodes", "/services", "/nodes/abc"} {
		t.Run(resource, func(t *testing.T) {
			rec := get(t, resource)

			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf(
					"GET %s = %d, want %d while Docker is unreachable",
					resource, rec.Code, http.StatusServiceUnavailable,
				)
			}
			if !strings.Contains(rec.Body.String(), "ENG001") {
				t.Errorf("GET %s body = %s, want the ENG001 problem", resource, rec.Body.String())
			}
		})
	}
}

// TestWebManifestIsSelfContainedAndRelative: a manifest resolves member URLs
// against its own URL, so relative ones work under CETACEAN_BASE_PATH and
// absolute ones address the origin root. It also checks each icon names a file
// the build will ship, since a missing one voids the whole manifest.
func TestWebManifestIsSelfContainedAndRelative(t *testing.T) {
	manifest := readWebManifest(t)

	relative := func(name, value string) {
		t.Helper()

		if value == "" {
			t.Errorf("%s is empty", name)

			return
		}

		if !strings.HasPrefix(value, "./") {
			t.Errorf(
				"%s = %q, want a ./-relative URL — an absolute one addresses the "+
					"origin root rather than the base path the deployment serves",
				name, value,
			)
		}
	}

	relative("start_url", manifest.StartURL)
	relative("scope", manifest.Scope)

	if len(manifest.Icons) == 0 {
		t.Error("the manifest declares no icons, so nothing can install it")
	}

	for _, icon := range manifest.Icons {
		relative("icon "+icon.Sizes, icon.Src)

		if _, err := os.Stat(path.Join(path.Dir(webManifestPath), icon.Src)); err != nil {
			t.Errorf("the manifest names %q but frontend/public holds no such file", icon.Src)
		}
	}

	// A browser offers to install only with both present.
	for _, size := range []string{"192x192", "512x512"} {
		if !slices.ContainsFunc(manifest.Icons, func(i webManifestIcon) bool {
			return i.Sizes == size
		}) {
			t.Errorf("no %s icon; a browser will not offer to install this", size)
		}
	}
}

// TestWebManifestThemeColorMatchesTheDocument pins two of the three places the
// theme colour is stated — a manifest holds one value, index.html a
// media-queried pair. The third is --background in index.css.
func TestWebManifestThemeColorMatchesTheDocument(t *testing.T) {
	manifest := readWebManifest(t)

	if manifest.ThemeColor == "" {
		t.Fatal("the manifest declares no theme_color")
	}

	raw, err := os.ReadFile("../../frontend/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}

	document := string(raw)

	light := strings.Index(document, `media="(prefers-color-scheme: light)"`)
	if light < 0 {
		t.Fatal("index.html declares no light-scheme theme-color")
	}

	// content follows media inside the same tag.
	tag := document[light:]
	if end := strings.IndexByte(tag, '>'); end >= 0 {
		tag = tag[:end]
	}

	if !strings.Contains(tag, `content="`+manifest.ThemeColor+`"`) {
		t.Errorf(
			"index.html's light theme-color does not match the manifest's "+
				"theme_color (%q); the tag reads: %s",
			manifest.ThemeColor, strings.Join(strings.Fields(tag), " "),
		)
	}
}

// TestSPAToleratesAbsentManifest covers the fallback for a dist built without
// the precompress plugin: every asset is served as identity.
func TestSPAToleratesAbsentManifest(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":        {Data: []byte("<html><head></head></html>")},
		"assets/app-abc.js": {Data: []byte("content")},
	}
	handler := NewSPAHandler(fsys, "")

	req := httptest.NewRequest("GET", "/assets/app-abc.js", nil)
	req.Header.Set("Accept-Encoding", "zstd, gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "content" {
		t.Errorf("body = %q, want the plain file", got)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want none without a manifest", got)
	}
	// Cache-Control's assets/ rule doesn't depend on the manifest either.
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable even without a manifest", got)
	}
}

// TestEmbeddedManifestMatchesEmbeddedFiles catches a manifest naming files the
// embed directive would not include, which is what happens if the manifest or
// its variants are produced under a dot- or underscore-prefixed path.
//
// It asserts against frontend/dist on disk rather than main.go's embedded FS,
// since the inclusion rule is a property of the path and so fails identically
// either way. It hashes every file rather than stat-ing it, because a manifest
// describes content — an existence check once let through variants compressed
// from a mid-build snapshot, which threw ReferenceError in the browser.
func TestEmbeddedManifestMatchesEmbeddedFiles(t *testing.T) {
	distDir := filepath.Join("..", "..", "frontend", "dist")
	manifestPath := filepath.Join(distDir, "assets-manifest.json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("reading %s: %v (run `npm run build` in frontend/ first)", manifestPath, err)
	}

	var manifest map[string]assetEntry
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parsing %s: %v", manifestPath, err)
	}

	if len(manifest) == 0 {
		t.Fatalf("%s has no entries", manifestPath)
	}

	suffixByCoding := map[string]string{"gzip": ".gz", "zstd": ".zst"}

	// The manifest stores bare hex; computeETag returns it quoted.
	etagOf := func(t *testing.T, path string) (string, bool) {
		t.Helper()

		body, err := os.ReadFile(path)
		if err != nil {
			return "", false
		}

		return strings.Trim(computeETag(body), `"`), true
	}

	for assetPath, entry := range manifest {
		identityPath := filepath.Join(distDir, filepath.FromSlash(assetPath))

		identityETag, ok := etagOf(t, identityPath)
		if !ok {
			t.Errorf("manifest names %s, but it is missing from frontend/dist", assetPath)
			continue
		}

		if identityETag != entry.ETag {
			t.Errorf(
				"manifest ETag for %s is %s, but the file on disk hashes to %s",
				assetPath,
				entry.ETag,
				identityETag,
			)
		}

		// The build's own threshold (frontend/plugins/precompress.ts) and the
		// server's (compressionThreshold) are two constants that have to agree.
		// A variant below the server's threshold would never be served.
		if info, err := os.Stat(identityPath); err == nil && info.Size() < compressionThreshold {
			t.Errorf(
				"manifest names %s at %d bytes, under the server's %d-byte threshold",
				assetPath, info.Size(), compressionThreshold,
			)
		}

		for coding, variant := range entry.Variants {
			suffix, known := suffixByCoding[coding]
			if !known {
				t.Errorf("manifest entry %s has unknown variant coding %q", assetPath, coding)
				continue
			}

			variantPath := identityPath + suffix

			variantETag, ok := etagOf(t, variantPath)
			if !ok {
				t.Errorf(
					"manifest names %s variant %q (%s), but it is missing from frontend/dist",
					assetPath,
					coding,
					variant.ETag,
				)
				continue
			}

			if variantETag != variant.ETag {
				t.Errorf(
					"manifest ETag for %s variant %q is %s, but the file on disk hashes to %s",
					assetPath,
					coding,
					variant.ETag,
					variantETag,
				)
			}
		}
	}
}

// TestEmbeddedManifestOmitsIndexHTML holds the build to the one file the SPA
// handler never routes through serveAsset: NewSPAHandler serves index.html with
// <base href> injected, so /index.html.gz would hand out the un-injected shell.
func TestEmbeddedManifestOmitsIndexHTML(t *testing.T) {
	distDir := filepath.Join("..", "..", "frontend", "dist")

	data, err := os.ReadFile(filepath.Join(distDir, "assets-manifest.json"))
	if err != nil {
		t.Fatalf("reading the manifest: %v (run `npm run build` in frontend/ first)", err)
	}

	var manifest map[string]assetEntry
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parsing the manifest: %v", err)
	}

	if _, listed := manifest["index.html"]; listed {
		t.Error("manifest lists index.html, which the SPA handler never serves from disk")
	}

	for _, suffix := range []string{".gz", ".zst"} {
		if _, err := os.Stat(filepath.Join(distDir, "index.html"+suffix)); err == nil {
			t.Errorf("frontend/dist holds index.html%s, which nothing serves", suffix)
		}
	}
}

// The catch-all serves one representation, text/html, so a client that ruled
// it out cannot be answered with it. negotiate strips an extension suffix and
// mutates r.URL.Path before routing, so an unmatched path arrives with the
// suffix gone and only the negotiation record left.
func TestUnroutedExtensionPathIsRefusedNotServedTheSPA(t *testing.T) {
	router := newTestRouterWithConfig(t, nil)

	cases := []struct {
		name   string
		path   string
		accept string
	}{
		{"a well-known .json URI", "/.well-known/jwks.json", "text/html"},
		{"a .json suffix", "/nonesuch.json", "text/html"},
		{"a .atom suffix", "/nonesuch.atom", "text/html"},
		// The suffix in an inner segment is the same probe.
		{"a suffix one segment in", "/nonesuch.atom/feed", "*/*"},
		// No suffix at all: the Accept header rules the shell out on its own.
		// One Accept-driven case, as proof the middleware reaches this route;
		// rangesAcceptHTML owns the rest of the matrix.
		{"an explicit JSON ask", "/nonesuch", "application/json"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set("Accept", tt.accept)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s = %d, want %d", tt.path, rec.Code, http.StatusNotFound)
			}

			if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
				t.Errorf("GET %s Content-Type = %q, want application/problem+json", tt.path, got)
			}
		})
	}
}

// The catch-all is the asset server and the client-side router both, so the
// refusal above must turn on the suffix alone. Every built asset is fetched
// with Accept: */*, which resolves to JSON.
func TestUnroutedPathWithoutExtensionStillServesTheSPA(t *testing.T) {
	router := newTestRouterWithConfig(t, nil)

	cases := []struct {
		name   string
		path   string
		accept string
	}{
		{"a client-side route", "/nonesuch", "text/html"},
		{"an asset probe", "/nonesuch", "*/*"},
		{"an explicit .html suffix", "/nonesuch.html", "text/html"},
		// Docker names may hold dots, and /services/:id/:subResource is a
		// real client-side route, so a dot alone cannot mean "not a route".
		{"a dotted service name", "/services/web.api/logs", "text/html"},
		{"a version-like service name", "/services/v1.2.3/logs", "text/html"},
		// `spa` is also the text/html arm of every routed endpoint, so a
		// dotted name that ends in a known suffix is a resource, not a probe.
		{"a service named for a format", "/services/web.json/logs", "text/html"},
		{"a preference that is not an exclusion", "/nonesuch", "application/json, */*;q=0.1"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set("Accept", tt.accept)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("GET %s = %d, want %d", tt.path, rec.Code, http.StatusOK)
			}

			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
				t.Errorf("GET %s Content-Type = %q, want the SPA shell", tt.path, got)
			}
		})
	}
}

// The rule the catch-all turns on, table-driven; see rangesAcceptHTML.
func TestRangesAcceptHTML(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"", true},
		{"*/*", true},
		{"text/*", true},
		{"text/html", true},
		{"text/html;q=0.1", true},
		{"application/json, */*;q=0.1", true},
		{"text/html, application/json", true},
		{"application/json", false},
		{"application/atom+xml", false},
		{"text/event-stream", false},
		// The most specific matching range carries the weight that applies.
		{"*/*, text/html;q=0", false},
		{"text/html;q=0", false},
		{"*/*;q=0", false},
	}

	for _, tt := range cases {
		t.Run(tt.accept, func(t *testing.T) {
			if got := rangesAcceptHTML(parseAcceptRanges(tt.accept)); got != tt.want {
				t.Errorf("rangesAcceptHTML(%q) = %v, want %v", tt.accept, got, tt.want)
			}
		})
	}
}

// A static file has one representation, so there is nothing to negotiate and
// Accept is not consulted (RFC 9110 §12.1). negotiate resolves against the
// API's media types, which name no file type at all: image/png is
// "unsupported" there while the file is right here.
func TestAssetIsServedWhateverTheClientAccepts(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n")

	router := newTestRouterWithConfig(t, []routerOption{func(cfg *RouterConfig) {
		cfg.SPA = NewSPAHandler(fstest.MapFS{
			"index.html":        {Data: []byte("<html></html>")},
			"favicon-32x32.png": {Data: png},
		}, "")
	}})

	for _, accept := range []string{"image/png", "image/*", "text/html", "*/*"} {
		t.Run(accept, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/favicon-32x32.png", nil)
			req.Header.Set("Accept", accept)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
			}

			if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
				t.Errorf("content-type=%q, want image/png", ct)
			}

			if !bytes.Equal(rec.Body.Bytes(), png) {
				t.Errorf("body=%q, want the file's bytes", rec.Body.Bytes())
			}
		})
	}
}

// 406 answers for a resource that exists and cannot be represented acceptably.
// Nothing exists on this route, so an Accept naming a type it never heard of
// earns the same 404 as one naming JSON.
func TestUnroutedPathIsNotFoundRatherThanUnacceptable(t *testing.T) {
	router := newTestRouterWithConfig(t, nil)

	for _, accept := range []string{"image/png", "image/*", "application/xml"} {
		t.Run(accept, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/nonesuch", nil)
			req.Header.Set("Accept", accept)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
			}

			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("content-type=%q, want application/problem+json", ct)
			}
		})
	}
}
