package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

type basePathCtxKey struct{}

var basePathKey = basePathCtxKey{}

type publicURLCtxKey struct{}

var publicURLKey = publicURLCtxKey{}

// BasePathFromContext extracts the base path from the context.
// Returns "" if not set.
func BasePathFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(basePathKey).(string); ok {
		return v
	}
	return ""
}

// PublicURLFromContext extracts the configured external origin.
// Returns "" when server.public_url is unset.
func PublicURLFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(publicURLKey).(string); ok {
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

// absURL builds a full absolute URL (scheme://host/base/path) from the
// request. Uses server.public_url when configured; otherwise
// X-Forwarded-Proto/Host, falling back to r.TLS and r.Host. Intended for Atom
// feeds where RFC 4287 requires IRIs.
func absURL(r *http.Request, path string) string {
	if base := PublicURLFromContext(r.Context()); base != "" {
		return base + absPath(r.Context(), path)
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}

	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}

	return scheme + "://" + host + absPath(r.Context(), path)
}

// publicURLMiddleware stores server.public_url in the request context so
// absURL builds links from configuration rather than from X-Forwarded-*,
// which any client can set. A no-op when public_url is unset.
func publicURLMiddleware(publicURL string, next http.Handler) http.Handler {
	if publicURL == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), publicURLKey, publicURL)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
