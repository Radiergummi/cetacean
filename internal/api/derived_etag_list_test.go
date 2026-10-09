package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// The services and tasks lists carry the derived validator, so a conditional
// request is answered before the list is read, filtered, sorted and encoded.
func TestServiceAndTaskListsUseTheDerivedValidator(t *testing.T) {
	c := cache.New(nil)
	c.SetService(
		swarm.Service{
			ID:   "svc1",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
		},
	)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1"})
	h := newTestHandlers(t, withCache(c))

	for path, handler := range map[string]http.HandlerFunc{
		"/services": h.HandleListServices,
		"/tasks":    h.HandleListTasks,
	} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		handler(w, req)

		validator := h.derivedETag(req)
		if etag := w.Header().Get("ETag"); !strings.Contains(etag, validator) {
			t.Errorf("%s: ETag = %q, want the derived validator %q", path, etag, validator)
		}

		conditional := httptest.NewRequest("GET", path, nil)
		conditional.Header.Set("Accept", "application/json")
		conditional.Header.Set("If-None-Match", w.Header().Get("ETag"))
		w = httptest.NewRecorder()
		handler(w, conditional)

		if w.Code != http.StatusNotModified {
			t.Errorf("%s: conditional status = %d, want 304", path, w.Code)
		}
	}
}
