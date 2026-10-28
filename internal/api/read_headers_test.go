package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Every GET reports Allow, and every JSON answer an ETag, read-only endpoints
// included; an endpoint that sends only JSON refuses anything else.
func TestReadOnlyEndpointsReportAllowAndETag(t *testing.T) {
	c := cache.New(nil)
	populateSpecFixtures(c)
	router := newTestRouterWithCache(t, c)

	get := func(target, accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		return rec
	}

	for _, target := range []string{
		"/cluster",
		"/cluster/capacity",
		"/history",
		"/search?q=svc",
		"/topology",
		"/recommendations",
		"/nodes/node1/tasks",
		"/services/svc1/tasks",
		"/nodes/node1/drain-impact",
		"/api/errors",
		"/api/errors/SVC001",
		"/metrics/status",
		"/auth/whoami",
	} {
		rec := get(target, "application/json")
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200: %s", target, rec.Code, rec.Body.String())
			continue
		}
		if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
			t.Errorf("GET %s: Allow = %q, want GET, HEAD", target, allow)
		}
		// whoami is per-identity and no-store: see skipValidatorCoverage.
		if rec.Header().Get("ETag") == "" && target != "/auth/whoami" {
			t.Errorf("GET %s sent no ETag", target)
		}
	}

	for _, target := range []string{"/swarm/unlock-key", "/metrics/labels", "/metrics/labels/job"} {
		if rec := get(target, "text/csv"); rec.Code != http.StatusNotAcceptable {
			t.Errorf("GET %s as text/csv = %d, want 406", target, rec.Code)
		}
	}
}
