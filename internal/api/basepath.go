package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

type basePathCtxKey struct{}

var basePathKey = basePathCtxKey{}

// BasePathFromContext extracts the base path from the context.
// Returns "" if not set.
func BasePathFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(basePathKey).(string); ok {
		return v
	}
	return ""
}

// absPath prepends the base path from context to path.
// If the base path is "", path is returned unchanged.
func absPath(ctx context.Context, path string) string {
	base := BasePathFromContext(ctx)
	if base == "" {
		return path
	}
	return base + path
}

// basePathMiddleware strips the base path prefix from incoming requests,
// stores the base path in context, and redirects trailing slashes.
// If basePath is "", it is a no-op.
func basePathMiddleware(basePath string, next http.Handler) http.Handler {
	if basePath == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !strings.HasPrefix(path, basePath+"/") && path != basePath {
			http.NotFound(w, r)
			return
		}

		stripped := path[len(basePath):]

		if stripped == "" || stripped == "/" {
			ctx := context.WithValue(r.Context(), basePathKey, basePath)
			r = r.WithContext(ctx)
			r.URL.Path = "/"
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasSuffix(stripped, "/") {
			trimmed := strings.TrimRight(stripped, "/")
			target := url.URL{
				Path:     basePath + trimmed,
				RawQuery: r.URL.RawQuery,
			}
			http.Redirect(w, r, target.String(), http.StatusMovedPermanently)
			return
		}

		ctx := context.WithValue(r.Context(), basePathKey, basePath)
		r = r.WithContext(ctx)
		r.URL.Path = stripped
		next.ServeHTTP(w, r)
	})
}
