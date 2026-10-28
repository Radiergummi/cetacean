package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	promapi "github.com/radiergummi/cetacean/internal/prometheus"
)

// PromQL reaches every service and node whatever the caller's grants, so the
// proxy and its stream answer only an identity that may read everything.
func TestMetricsProxyRequiresReadOnEverything(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer prom.Close()

	requests := map[string]string{
		"/metrics?query=up":        "application/json",
		"/metrics?query=up&step=5": "text/event-stream",
		"/metrics/labels":          "application/json",
		"/metrics/labels/job":      "application/json",
	}

	for name, tc := range map[string]struct {
		resources []string
		want      int
	}{
		"stack-scoped":     {[]string{"stack:web"}, http.StatusForbidden},
		"service and node": {[]string{"service:*", "node:*"}, http.StatusForbidden},
		"bare wildcard":    {[]string{"*"}, http.StatusOK},
	} {
		evaluator := acl.NewEvaluator()
		evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
			Resources:   tc.resources,
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		}}})

		router := newTestRouterWithConfig(
			t,
			[]routerOption{
				func(cfg *RouterConfig) {
					cfg.MetricsProxy = promapi.NewProxy(prom.URL)
				},
			},
			withCache(cache.New(nil)),
			withACL(evaluator),
			withPromClient(promapi.NewClient(prom.URL)),
		)

		for target, accept := range requests {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			req.Header.Set("Accept", accept)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			cancel()

			if w.Code != tc.want {
				t.Errorf("%s: %s (%s) = %d, want %d: %s",
					name, target, accept, w.Code, tc.want, w.Body.String())
			}
			if tc.want == http.StatusForbidden && !strings.Contains(w.Body.String(), "ACL001") {
				t.Errorf("%s: %s refused without ACL001: %s", name, target, w.Body.String())
			}
			if !slices.Contains(w.Header().Values("Vary"), varyIdentity) {
				t.Errorf("%s: %s does not vary by identity: %q",
					name, target, w.Header().Values("Vary"))
			}
		}
	}
}
