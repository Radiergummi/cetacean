package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// spelling rewrites a request into another that names the same resource. A
// loose one may be refused where the canonical path is not, or redirected to
// it: the mux answers a dot segment with a redirect only after auth has run.
type spelling struct {
	name  string
	spell func(method, path string) (string, string, bool)
	loose bool
}

func spellings() []spelling {
	out := []spelling{
		{name: "HEAD", spell: func(m, p string) (string, string, bool) {
			return http.MethodHead, p, m == http.MethodGet
		}},
		{name: "percent-encoded", spell: func(m, p string) (string, string, bool) {
			i := strings.LastIndexByte(p, '/') + 1
			if i >= len(p) || !isLetter(p[i]) {
				return "", "", false
			}

			return m, fmt.Sprintf("%s%%%02X%s", p[:i], p[i], p[i+1:]), true
		}},
		{name: "double slash", loose: true, spell: func(m, p string) (string, string, bool) {
			return m, "/" + p, p != "/"
		}},
		{name: "dot segment", loose: true, spell: func(m, p string) (string, string, bool) {
			return m, "/." + p, p != "/"
		}},
	}

	for _, ext := range extensionTypes {
		out = append(out, spelling{name: ext.ext, spell: func(m, p string) (string, string, bool) {
			return m, p + ext.ext, scopedTo(ext.under, p) && !literalDocuments[p+ext.ext]
		}})
	}

	return out
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

type decision struct{ refused, forbidden, reflected bool }

// looser reports whether d lets through something canonical does not.
func (d decision) looser(canonical decision) bool {
	return !d.refused && canonical.refused ||
		!d.forbidden && canonical.forbidden ||
		d.reflected && !canonical.reflected
}

// Every middleware ahead of the mux decides on a path, so each has to see the
// one the mux routes: two requests naming one resource get one answer from
// authentication, CORS and cross-origin protection. Driven through the whole
// chain, over every route, from an allowed origin and a foreign one.
func TestEverySpellingOfARouteGetsTheSameDecision(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc9700/no-cors-at-the-authorization-endpoint")

	const allowed, foreign = "https://example.test", "https://elsewhere.test"

	build := func(opts ...routerOption) http.Handler {
		return newTestRouterWithConfig(t, append([]routerOption{
			withCORS(allowed),
			func(cfg *RouterConfig) { cfg.AuthProvider = &refusingProvider{} },
		}, opts...))
	}

	router := build(withOAuthRoutes(""))
	prefixed := build(withBasePath("/cetacean"), withOAuthRoutes("/cetacean"))

	send := func(h http.Handler, method, path, origin string, preflight bool) (decision, string) {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", "cross-site")

		if preflight {
			r.Header.Set("Access-Control-Request-Method", http.MethodPost)
		}

		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		var location string
		if w.Code >= 300 && w.Code < 400 {
			location = w.Header().Get("Location")
		}

		return decision{
			refused:   w.Code == http.StatusUnauthorized,
			forbidden: w.Code == http.StatusForbidden,
			reflected: w.Header().Get("Access-Control-Allow-Origin") != "",
		}, location
	}

	// internal/oauth registers these on the mux directly, past the recorder.
	patterns := append(routerPatterns(t),
		"GET /oauth/authorize", "POST /oauth/authorize", "POST /oauth/token")

	for _, pattern := range patterns {
		method, path := routeOf(pattern)

		for _, origin := range []string{allowed, foreign} {
			for _, preflight := range []bool{false, true} {
				m := method
				if preflight {
					m = http.MethodOptions
				}

				canonical, _ := send(router, m, path, origin, preflight)

				if got, _ := send(
					prefixed,
					m,
					"/cetacean"+path,
					origin,
					preflight,
				); got != canonical {
					t.Errorf("%s /cetacean%s from %s = %+v, but %s %s = %+v",
						m, path, origin, got, m, path, canonical)
				}

				for _, s := range spellings() {
					sm, sp, ok := s.spell(m, path)
					if !ok || (preflight && sm != http.MethodOptions) {
						continue
					}

					got, location := send(router, sm, sp, origin, preflight)

					switch {
					case s.loose && location != "" && strings.TrimPrefix(location, "http://example.com") == path:
					case s.loose && !got.looser(canonical):
					case !s.loose && got == canonical:
					default:
						t.Errorf("%s %s (%s) from %s = %+v, but %s %s = %+v",
							sm, sp, s.name, origin, got, m, path, canonical)
					}
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
