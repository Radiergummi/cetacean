package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/spec"
)

// The validated version must reach the engine with the write, so a change
// landing after the check is refused by the engine and answered as the 412 the
// check would have given.
func TestPreconditionPinsTheWrite(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/a-false-if-match-stops-the-method")

	sequenceConflict := errors.New("rpc error: code = Unknown desc = update out of sequence")

	type outcome struct {
		pinned  swarm.Version
		ok      bool
		written bool
	}

	newServer := func(t *testing.T, writeErr error) (http.Handler, string, *outcome) {
		t.Helper()

		seen := &outcome{}
		c := cache.New(nil)
		c.SetService(swarm.Service{
			ID:      "svc1",
			Version: swarm.Version{Index: 7},
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "web"},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{Env: []string{"A=1"}},
				},
			},
		})
		wc := &mockWriteClient{
			updateServiceEnvFn: func(
				ctx context.Context,
				id string,
				_ map[string]string,
			) (swarm.Service, error) {
				seen.written = true
				seen.pinned, seen.ok = docker.PinnedVersion(ctx, "service", id)
				svc, _ := c.GetService(id)

				return svc, writeErr
			},
		}
		router := newTestRouterWithCache(t, c, withWriteClient(wc))

		req := httptest.NewRequest("GET", "/services/svc1/env", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		return router, rec.Header().Get("ETag"), seen
	}

	patch := func(router http.Handler, ifMatch string) int {
		req := httptest.NewRequest("PATCH", "/services/svc1/env", strings.NewReader(`{"B":"2"}`))
		req.Header.Set("Content-Type", "application/merge-patch+json")
		req.Header.Set("Accept", "application/json")
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		return rec.Code
	}

	t.Run("a matching If-Match pins the version it validated", func(t *testing.T) {
		router, etag, seen := newServer(t, nil)
		if got := patch(router, etag); got != http.StatusOK {
			t.Fatalf("status = %d, want 200", got)
		}
		if !seen.ok || seen.pinned.Index != 7 {
			t.Errorf("pinned = %v (%v), want index 7", seen.pinned, seen.ok)
		}
	})

	t.Run("no If-Match pins nothing", func(t *testing.T) {
		router, _, seen := newServer(t, nil)
		if got := patch(router, ""); got != http.StatusOK {
			t.Fatalf("status = %d, want 200", got)
		}
		if seen.ok {
			t.Errorf("pinned %v without a precondition", seen.pinned)
		}
	})

	t.Run("the engine refusing the pinned version answers 412", func(t *testing.T) {
		router, etag, seen := newServer(t, sequenceConflict)
		if got := patch(router, etag); got != http.StatusPreconditionFailed {
			t.Errorf("status = %d, want 412", got)
		}
		if !seen.written {
			t.Error("the write was never attempted")
		}
	})

	t.Run("without If-Match a sequence conflict stays 409", func(t *testing.T) {
		router, _, _ := newServer(t, sequenceConflict)
		if got := patch(router, ""); got != http.StatusConflict {
			t.Errorf("status = %d, want 409", got)
		}
	})
}
