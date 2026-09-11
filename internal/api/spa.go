package api

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// hasMidPathExtension reports whether the URL path contains a dot-extension
// in a non-terminal segment, e.g. /foo.atom/feed or /data.json/bar.
// Such paths are never valid client-side routes.
func hasMidPathExtension(path string) bool {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for _, seg := range segments[:len(segments)-1] {
		if strings.Contains(seg, ".") {
			return true
		}
	}
	return false
}

// assetVariant is one precompressed representation of a built asset, as
// recorded by the Vite precompress plugin (frontend/plugins/precompress.ts).
type assetVariant struct {
	ETag string `json:"etag"`
}

// assetEntry is one manifest record. The plugin stores ETags as bare hex, so
// serveAsset adds the quotes itself. A variant's ETag hashes the compressed
// bytes, unlike codedETag's suffixed hash of the identity ones: no asset is
// ever an If-Match target, so nothing here needs the base hash back.
type assetEntry struct {
	ETag     string                  `json:"etag"`
	Variants map[string]assetVariant `json:"variants"`
}

// loadAssetManifest reads assets-manifest.json from fsys, keyed by asset path
// relative to fsys with no leading slash. A missing or unparsable manifest
// yields a nil map, and every asset is then served as identity.
func loadAssetManifest(fsys fs.FS) map[string]assetEntry {
	data, err := fs.ReadFile(fsys, "assets-manifest.json")
	if err != nil {
		return nil
	}

	var manifest map[string]assetEntry
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil
	}

	return manifest
}

// variantSuffix is the file suffix precompress.ts emits for a coding, e.g.
// "assets/app-abc.js.zst". The manifest's variant keys ("gzip"/"zstd", from
// Encoding.String()) differ from these suffixes, so this is not derived from it.
func variantSuffix(e Encoding) string {
	switch e {
	case EncodingGzip:
		return ".gz"
	case EncodingZstd:
		return ".zst"
	default:
		return ""
	}
}

// assetCacheControl returns the Cache-Control value for a non-index asset path.
// Hashed filenames under assets/ never change, so they are cached immutably;
// everything else keeps the server's default.
func assetCacheControl(path string) string {
	if strings.HasPrefix(path, "assets/") {
		return "public, max-age=31536000, immutable"
	}

	return ""
}

// serveAsset serves the file at path, choosing the best precompressed
// variant for the request's Accept-Encoding when the manifest has one.
func serveAsset(
	w http.ResponseWriter,
	r *http.Request,
	fsys fs.FS,
	path string,
	manifest map[string]assetEntry,
) {
	servePath := path
	coding := EncodingIdentity
	etag := ""

	if entry, ok := manifest[path]; ok {
		etag = entry.ETag

		if negotiated := resolveEncoding(r); negotiated != EncodingIdentity {
			if variant, ok := entry.Variants[negotiated.String()]; ok {
				coding = negotiated
				etag = variant.ETag
				servePath = path + variantSuffix(negotiated)
			}
		}

		w.Header().Add("Vary", "Accept-Encoding")
	}

	f, err := fsys.Open(servePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close() //nolint:errcheck

	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Set Content-Type from the original path's extension, or http.ServeContent
	// sniffs the compressed variant's bytes and reports application/gzip — a
	// statement about the coding, not the resource. Identity is left to sniff,
	// where the bytes are the resource.
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" && coding != EncodingIdentity {
		contentType = "application/octet-stream"
	}

	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	if etag != "" {
		w.Header().Set("ETag", `"`+etag+`"`)
	}

	if coding != EncodingIdentity {
		w.Header().Set("Content-Encoding", coding.String())
	}

	if cc := assetCacheControl(path); cc != "" {
		w.Header().Set("Cache-Control", cc)
	}

	var modTime time.Time
	if info, err := f.Stat(); err == nil {
		modTime = info.ModTime()
	}

	http.ServeContent(w, r, path, modTime, rs)
}

func NewSPAHandler(fsys fs.FS, basePath string) http.Handler {
	// Read and prepare index.html with base path injection.
	indexBytes, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		panic(fmt.Sprintf("spa: embedded index.html missing: %v", err))
	}
	indexHTML := string(indexBytes)

	baseHref := "/"
	if basePath != "" {
		baseHref = basePath + "/"
	}

	safeHref := html.EscapeString(baseHref)
	injection := `<base href="` + safeHref + `">` + "\n" +
		`    <link rel="canonical" href="` + safeHref + `">`

	indexHTML = strings.Replace(indexHTML, "<head>", "<head>\n    "+injection, 1)
	preparedIndex := []byte(indexHTML)

	manifest := loadAssetManifest(fsys)

	writeIndex := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(preparedIndex)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This handler is the mux's catch-all, so it answers every path no
		// route matched. It has one representation — a document — and
		// serving it to a POST, PUT or DELETE tells a client its write
		// succeeded when nothing was registered to receive it. A wrong method
		// on a path that *does* exist is still ServeMux's 405.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeProblem(w, r, http.StatusNotFound, "no such resource")
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// A directory is not an asset: serveAsset cannot seek one, and the
		// path is treated as a client-side route, exactly as "/assets/" is.
		info, err := fs.Stat(fsys, path)
		if err != nil || info.IsDir() {
			// Reject paths that look like subpaths of an extension
			// (e.g., /nodes.atom/feed/, /data.json/foo). These are feed
			// reader discovery probes, not client-side routes. Returning
			// 200 with HTML confuses feed autodiscovery.
			if hasMidPathExtension(r.URL.Path) {
				http.NotFound(w, r)
				return
			}

			// Fall back to prepared index.html for client-side routing.
			writeIndex(w)
			return
		}

		// For index.html itself, serve the prepared version.
		if path == "index.html" {
			writeIndex(w)
			return
		}

		serveAsset(w, r, fsys, path, manifest)
	})
}
