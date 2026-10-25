package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// The router hands every write the caller as its actor, and names the
// transport by what the browser says of itself.
func TestWritesCarryTheirActor(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    cache.Transport
	}{
		{"dashboard", map[string]string{"Sec-Fetch-Site": "same-origin"}, cache.TransportDashboard},
		{"plain client", nil, cache.TransportREST},
		{"allowed cross-origin page", map[string]string{
			"Sec-Fetch-Site": "cross-site",
			"Origin":         "https://app.example",
		}, cache.TransportREST},
		{"bearer token", map[string]string{
			"Sec-Fetch-Site": "same-origin",
			"Authorization":  "Bearer abc",
		}, cache.TransportREST},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := cache.New(nil)
			c.SetService(replicatedService("svc1"))

			var (
				actor    cache.Actor
				recorded bool
			)

			wc := &mockWriteClient{
				scaleServiceFn: func(ctx context.Context, id string, _ uint64) (swarm.Service, error) {
					actor, recorded = cache.ActorFromContext(ctx)
					return replicatedService(id), nil
				},
			}
			router := newTestRouterWithConfig(
				t,
				[]routerOption{withCORS("https://app.example")},
				withCache(c),
				withWriteClient(wc),
				withOpsLevel(config.OpsImpactful),
			)

			req := httptest.NewRequest(
				http.MethodPut,
				"/services/svc1/scale",
				strings.NewReader(`{"replicas":3}`),
			)
			req.Header.Set("Content-Type", "application/json")
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body)
			}

			if !recorded {
				t.Fatal("the write reached the writer with no actor")
			}

			want := cache.Actor{
				Subject:   "anonymous",
				Name:      "Anonymous",
				Provider:  "none",
				Transport: tc.want,
			}
			if actor != want {
				t.Errorf("actor = %+v, want %+v", actor, want)
			}
		})
	}
}
