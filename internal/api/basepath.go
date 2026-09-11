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
// request. Uses server.public_url when configured, and otherwise the origin
// the request itself names. Intended for documents that must carry absolute
// URLs — Atom feeds, where RFC 4287 requires IRIs, and the discovery documents
// under /.well-known.
func absURL(r *http.Request, path string) string {
	if base := PublicURLFromContext(r.Context()); base != "" {
		return base + absPath(r.Context(), path)
	}

	scheme, host := requestOrigin(r)

	return scheme + "://" + host + absPath(r.Context(), path)
}

// requestOrigin resolves the scheme and authority a client reached this
// request on, for use when server.public_url is unset.
//
// A proxy's account of the original connection is believed only when
// auth.FromTrustedProxy vouches for the peer — the verdict realIP recorded at
// the edge, on the address the connection actually arrived from. Forwarded and
// X-Forwarded-Proto are just request headers otherwise, and these values end
// up in URLs Cetacean publishes.
//
// Trusting the peer is not the same as trusting the value, so both are
// validated: forwarding a client's own Host into X-Forwarded-Host is a common
// proxy configuration, which would otherwise put an arbitrary string where an
// authority belongs.
//
// r.Host is the remaining fallback and is itself whatever the client asked
// for. A server behind no proxy cannot learn its own name from the network, so
// the only way to publish links that do not depend on the caller is to
// configure server.public_url.
func requestOrigin(r *http.Request) (scheme, host string) {
	scheme = "http"
	if r.TLS != nil {
		scheme = "https"
	}

	host = r.Host

	if !auth.FromTrustedProxy(r.Context()) {
		return scheme, host
	}

	// RFC 7239 standardizes the pair below it, so a proxy sending both is
	// taken at its Forwarded word.
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

// isAuthority reports whether s can stand as the authority of a URL: non-empty
// and free of the delimiters that would end it early, which is what separates
// a hostname from a string that rewrites the rest of the link.
func isAuthority(s string) bool {
	if s == "" {
		return false
	}

	return !strings.ContainsAny(s, "/?#\\ \t\r\n\v\f\x00@")
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
