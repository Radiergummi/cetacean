package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
)

// A task's name and its parents' names belong to the parents, so a caller
// granted the task alone sees neither the service's name nor the node's.
func TestTaskResponsesNameOnlyReadableParents(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "hidden-host"},
	})
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "hidden-svc"}},
	})
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1", NodeID: "node1", Slot: 1})

	get := func(t *testing.T, path string, grants ...string) string {
		t.Helper()

		e := acl.NewEvaluator()
		e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
			Resources: grants, Audience: []string{"*"}, Permissions: []string{"read"},
		}}})
		e.SetResolver(c)
		router := newTestRouterWithCache(t, c, withACL(e))

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d", path, rec.Code)
		}

		body, _ := io.ReadAll(rec.Body)

		return string(body)
	}

	for _, path := range []string{
		"/tasks.json", "/tasks.csv", "/tasks/task1.json", "/tasks/task1.atom",
	} {
		t.Run(path, func(t *testing.T) {
			body := get(t, path, "task:*")
			for _, hidden := range []string{"hidden-svc", "hidden-host"} {
				if strings.Contains(body, hidden) {
					t.Errorf("a task:*-only caller sees %q:\n%s", hidden, body)
				}
			}
		})
	}

	t.Run("node tasks", func(t *testing.T) {
		body := get(t, "/nodes/node1/tasks.json", "task:*", "node:*")
		if strings.Contains(body, "hidden-svc") {
			t.Errorf("a caller without the service sees its name:\n%s", body)
		}
	})

	t.Run("service tasks", func(t *testing.T) {
		body := get(t, "/services/svc1/tasks.json", "task:*", "service:*")
		if strings.Contains(body, "hidden-host") {
			t.Errorf("a caller without the node sees its hostname:\n%s", body)
		}
	})

	t.Run("readable parents stay named", func(t *testing.T) {
		body := get(t, "/tasks/task1.atom", "*")
		if !strings.Contains(body, "hidden-svc.1") {
			t.Errorf("a caller reading everything lost the task's name:\n%s", body)
		}
	})
}
