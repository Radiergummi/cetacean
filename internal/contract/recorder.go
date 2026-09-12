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
// request matches. It cannot read http.Request.Pattern off the served request
// -- the middleware chain shallow-copies it, so the real mux sets Pattern on a
// copy -- so it routes against a second mux built from the inventory's own
// patterns. A pattern a real mux will not accept then panics here.
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
