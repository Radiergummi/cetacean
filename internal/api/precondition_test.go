package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestPreconditionOnServiceEnv(t *testing.T) {
	newServer := func(t *testing.T) (http.Handler, string) {
		t.Helper()
		c := cache.New(nil)
		c.SetService(swarm.Service{
			ID:   "svc1",
			Meta: swarm.Meta{Version: swarm.Version{Index: 7}},
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "web"},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{Env: []string{"A=1"}},
				},
			},
		})
		router := newTestRouterWithCache(t, c)

		req := httptest.NewRequest("GET", "/services/svc1/env", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status = %d, want 200", rec.Code)
		}
		return router, rec.Header().Get("ETag")
	}

	patch := func(t *testing.T, router http.Handler, ifMatch string) int {
		t.Helper()
		req := httptest.NewRequest("PATCH", "/services/svc1/env",
			strings.NewReader(`{"B":"2"}`))
		req.Header.Set("Content-Type", "application/merge-patch+json")
		req.Header.Set("Accept", "application/json")
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	t.Run("matching If-Match is admitted", func(t *testing.T) {
		router, etag := newServer(t)
		if got := patch(t, router, etag); got == http.StatusPreconditionFailed {
			t.Errorf("status = 412 with a matching If-Match")
		}
	})

	t.Run("stale If-Match is refused", func(t *testing.T) {
		router, _ := newServer(t)
		if got := patch(t, router, `"stale"`); got != http.StatusPreconditionFailed {
			t.Errorf("status = %d, want 412", got)
		}
	})

	t.Run("weak validator is refused", func(t *testing.T) {
		router, etag := newServer(t)
		weak := `W/` + etag
		if got := patch(t, router, weak); got != http.StatusPreconditionFailed {
			t.Errorf("status = %d, want 412 — weak validators never match If-Match", got)
		}
	})

	t.Run("absent If-Match changes nothing", func(t *testing.T) {
		router, _ := newServer(t)
		if got := patch(t, router, ""); got == http.StatusPreconditionFailed {
			t.Error("status = 412 with no If-Match header")
		}
	})

	t.Run("wildcard on a missing resource is 412 not 404", func(t *testing.T) {
		router := newTestRouterWithCache(t, cache.New(nil))
		req := httptest.NewRequest("PATCH", "/services/gone/env",
			strings.NewReader(`{"B":"2"}`))
		req.Header.Set("Content-Type", "application/merge-patch+json")
		req.Header.Set("If-Match", "*")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusPreconditionFailed {
			t.Errorf("status = %d, want 412 (RFC 9110 §13.2.2)", rec.Code)
		}
	})
}
