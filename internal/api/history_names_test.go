package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Task entries keep the ID as their name for permission checks; what the API
// shows is the name Swarm gives the task, as MCP already did.
func TestHandleHistoryNamesTasksTheWaySwarmDoes(t *testing.T) {
	c := cache.New(nil)
	c.SetService(
		swarm.Service{
			ID:   "svc1",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "shop_web"}},
		},
	)
	c.SetTask(swarm.Task{ID: "wdqyr99x2amp6lf383k3fwb5g", ServiceID: "svc1", Slot: 1})
	h := newTestHandlers(t, withCache(c))

	req := httptest.NewRequest("GET", "/history", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"name":"shop_web.1"`) {
		t.Errorf("history does not name the task shop_web.1: %s", w.Body.String())
	}
}
