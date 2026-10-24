package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

// These representations list the services using the resource, filtered by the
// caller's grants; neither a service attaching nor a policy reload moves the
// resource's own timestamp, so a date validator would answer 304 for a stale body.
func TestARepresentationWithCrossReferencesSendsNoLastModified(t *testing.T) {
	then := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	meta := swarm.Meta{CreatedAt: then, UpdatedAt: then}

	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "front", Created: then})
	c.SetVolume(volume.Volume{Name: "data", CreatedAt: then.Format(time.RFC3339)})
	c.SetConfig(swarm.Config{ID: "cfg1", Meta: meta, Spec: swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: "app"},
	}})
	c.SetSecret(swarm.Secret{ID: "sec1", Meta: meta, Spec: swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: "key"},
	}})
	c.SetService(swarm.Service{ID: "svc1", Meta: meta, Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "web"},
	}})
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1", Meta: meta})
	router := newTestRouterWithCache(t, c)

	for _, path := range []string{
		"/networks/net1", "/volumes/data", "/configs/cfg1", "/secrets/sec1", "/tasks/task1",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "application/json")
			req.Header.Set("If-Modified-Since", time.Now().UTC().Format(http.TimeFormat))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
			}
			if got := w.Header().Get("Last-Modified"); got != "" {
				t.Errorf("Last-Modified = %q, want none", got)
			}
		})
	}
}
