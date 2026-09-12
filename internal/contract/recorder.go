package contract

import (
	"net/http"
	"slices"
	"sync"
)

// recorded holds every route pattern a sweep in this package has reached. It is
// package-level because the question — "did anything exercise this route?" — is
// about the whole test binary, not one test.
var (
	recordedMu sync.Mutex
	recorded   = map[string]bool{}
)

// patternMux is a second ServeMux registered with the route inventory's own
// patterns, used only to name the route a request matches.
var patternMux = sync.OnceValues(func() (*http.ServeMux, error) {
	routes, err := Routes()
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	for _, route := range routes {
		mux.Handle(route.String(), noop)
	}

	return mux, nil
})

// recordPatterns wraps the router and records which inventory route each
// request matches, so the package can be asked afterwards what it covered.
//
// It cannot read http.Request.Pattern off the request the real router served:
// NewRouter's middleware chain calls r.WithContext, which shallow-copies the
// request, so the real mux sets Pattern on a copy this wrapper never sees. It
// routes against a second ServeMux registered with the route inventory's own
// patterns instead — immune to that, and with a useful side effect: a pattern
// the inventory found that a real mux will not accept panics here rather than
// passing unnoticed.
func recordPatterns(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mux, err := patternMux(); err == nil {
			if _, pattern := mux.Handler(r); pattern != "" {
				recordedMu.Lock()
				recorded[pattern] = true
				recordedMu.Unlock()
			}
		}

		next.ServeHTTP(w, r)
	})
}

// exercisedRoutes returns the patterns reached so far, sorted.
func exercisedRoutes() []string {
	recordedMu.Lock()
	defer recordedMu.Unlock()

	out := make([]string, 0, len(recorded))
	for pattern := range recorded {
		out = append(out, pattern)
	}

	slices.Sort(out)

	return out
}
