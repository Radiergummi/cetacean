package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
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
// request, for documents that must carry them: Atom feeds and the discovery
// documents under /.well-known.
func absURL(r *http.Request, path string) string {
	return originOf(r) + absPath(r.Context(), path)
}

// originOf returns the scheme and authority absURL builds on. A document
// naming many URIs resolves it once and appends absPath itself.
func originOf(r *http.Request) string {
	if base := PublicURLFromContext(r.Context()); base != "" {
		return base
	}

	scheme, host := requestOrigin(r)

	return scheme + "://" + host
}

// requestOrigin resolves the origin a client reached this request on, when
// server.public_url is unset.
//
// A proxy's headers are believed only when auth.FromTrustedProxy vouches for
// the peer, and the values are validated even then: forwarding a client's own
// Host into X-Forwarded-Host is a common proxy configuration.
//
// r.Host is the remaining fallback and is also the client's. Only
// server.public_url gives links that do not depend on the caller.
func requestOrigin(r *http.Request) (scheme, host string) {
	scheme = "http"
	if r.TLS != nil {
		scheme = "https"
	}

	host = r.Host

	if !auth.FromTrustedProxy(r.Context()) {
		return scheme, host
	}

	// RFC 7239 standardizes the pair below it, so Forwarded wins.
	forwardedProto, forwardedHost := forwardedOrigin(r.Header.Values("Forwarded"))

	if forwardedProto == "" {
		forwardedProto = r.Header.Get("X-Forwarded-Proto")
	}

	if forwardedHost == "" {
		forwardedHost = r.Header.Get("X-Forwarded-Host")
	}

	if forwardedProto == "http" || forwardedProto == "https" {
		scheme = forwardedProto
	}

	if isAuthority(forwardedHost) {
		host = forwardedHost
	}

	return scheme, host
}

// isAuthority reports whether s can stand as a URL authority. Anything beyond
// one — a path, query, fragment, userinfo, control character — lands somewhere
// other than Host and fails the round-trip.
func isAuthority(s string) bool {
	if s == "" {
		return false
	}

	parsed, err := url.Parse("//" + s)

	return err == nil && parsed.Host == s && parsed.User == nil
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
