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
