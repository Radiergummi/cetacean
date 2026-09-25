package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// A suffix negotiate strips names the same resource, so no rule decided ahead
// of the mux may answer it differently: every middleware has to see the path
// the mux will route. Driven through the whole chain, over every route.
func TestASuffixChangesNeitherAuthenticationNorCORS(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc9700/no-cors-at-the-authorization-endpoint")

	const origin = "https://example.test"

	router := newTestRouterWithConfig(t, []routerOption{
		withCORS(origin),
		withOAuthRoutes(""),
		func(cfg *RouterConfig) { cfg.AuthProvider = &refusingProvider{} },
	})

	// internal/oauth registers these on the mux directly, past the recorder.
	patterns := append(routerPatterns(t),
		"GET /oauth/authorize", "POST /oauth/authorize", "POST /oauth/token")

	type outcome struct{ refused, reflected bool }

	send := func(method, path string) outcome {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Origin", origin)

		if method == http.MethodOptions {
			r.Header.Set("Access-Control-Request-Method", http.MethodPost)
		}

		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)

		return outcome{
			refused:   w.Code == http.StatusUnauthorized,
			reflected: w.Header().Get("Access-Control-Allow-Origin") != "",
		}
	}

	for _, pattern := range patterns {
		method, path := routeOf(pattern)

		for _, ext := range extensionTypes {
			if !scopedTo(ext.under, path) || literalDocuments[path+ext.ext] {
				continue
			}

			for _, m := range []string{method, http.MethodOptions} {
				if bare, suffixed := send(m, path), send(m, path+ext.ext); bare != suffixed {
					t.Errorf("%s %s%s = %+v, but %s %s = %+v",
						m, path, ext.ext, suffixed, m, path, bare)
				}
			}
		}
	}
}

var wildcard = regexp.MustCompile(`\{[^}]*\}`)

// routeOf turns a mux pattern into a request that matches it.
func routeOf(pattern string) (method, path string) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		method, path = http.MethodGet, pattern
	}

	path = strings.TrimSuffix(wildcard.ReplaceAllString(path, "x"), "/")
	if path == "" {
		path = "/"
	}

	return method, path
}
