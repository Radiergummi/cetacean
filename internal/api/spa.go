package api

import (
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"strings"
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

func NewSPAHandler(fsys fs.FS, basePath string) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))

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

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		f, err := fsys.Open(path)
		if err != nil {
			// Reject paths that look like subpaths of an extension
			// (e.g., /nodes.atom/feed/, /data.json/foo). These are feed
			// reader discovery probes, not client-side routes. Returning
			// 200 with HTML confuses feed autodiscovery.
			if hasMidPathExtension(r.URL.Path) {
				http.NotFound(w, r)
				return
			}

			// Fall back to prepared index.html for client-side routing.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(preparedIndex)
			return
		}
		_ = f.Close()

		// For index.html itself, serve the prepared version.
		if path == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(preparedIndex)
			return
		}

		// All other static files served as-is.
		fileServer.ServeHTTP(w, r)
	})
}
