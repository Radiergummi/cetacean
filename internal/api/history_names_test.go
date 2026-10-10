package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

func historyNamesCache() *cache.Cache {
	c := cache.New(nil)
	c.SetService(
		swarm.Service{
			ID:   "svc1",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "shop_web"}},
		},
	)
	c.SetTask(swarm.Task{ID: "wdqyr99x2amp6lf383k3fwb5g", ServiceID: "svc1", Slot: 1})

	return c
}

// Task entries keep the ID as their name for permission checks; what the API
// shows is the name Swarm gives the task, as MCP already did.
func TestHandleHistoryNamesTasksTheWaySwarmDoes(t *testing.T) {
	h := newTestHandlers(t, withCache(historyNamesCache()))

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

// A grant on the service reaches its tasks through the task ID, so naming
// before filtering would drop every task entry this caller may read.
func TestHandleHistoryNamesTasksAfterTheACLFilter(t *testing.T) {
	c := historyNamesCache()
	e := acl.NewEvaluator()
	e.SetResolver(c)
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:shop_web"},
		Audience:    []string{"user:alice"},
		Permissions: []string{"read"},
	}}})
	h := newTestHandlers(t, withCache(c), withACL(e))

	req := httptest.NewRequest("GET", "/history?type=task", nil)
	req.Header.Set("Accept", "application/json")
	req = req.WithContext(
		auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "alice"}),
	)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"name":"shop_web.1"`) {
		t.Errorf("history drops the task entry alice may read: %s", w.Body.String())
	}
}

func TestHistoryFeedNamesTasksTheWaySwarmDoes(t *testing.T) {
	h := newTestHandlers(t, withCache(historyNamesCache()))

	req := withContentType(httptest.NewRequest("GET", "/history", nil), ContentTypeAtom)
	w := httptest.NewRecorder()
	h.handleFeedHistory(w, req, renderAtom)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "task: shop_web.1") {
		t.Errorf("feed does not name the task shop_web.1: %s", w.Body.String())
	}
}
