package mcp

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// drainUpdated empties the session's notification channel, returning the URIs
// of the resources/updated notifications it held.
func drainUpdated(t *testing.T, name string) []string {
	t.Helper()

	ch := session(name).(stubSession).notifications

	var uris []string
	for {
		select {
		case n := <-ch:
			if n.Method != "notifications/resources/updated" {
				continue
			}
			uri, _ := n.Params.AdditionalFields["uri"].(string)
			uris = append(uris, uri)
		default:
			return uris
		}
	}
}

func stackService(id, name, stack string) swarm.Service {
	return swarm.Service{
		ID: id,
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
			Name:   name,
			Labels: map[string]string{"com.docker.stack.namespace": stack},
		}},
	}
}

func assertUpdated(t *testing.T, got []string, want ...string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("updated = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("updated = %v, want %v", got, want)
		}
	}
}

func TestSubscriptionStackFollowsMembers(t *testing.T) {
	c := cache.New(nil)
	c.SetService(stackService("svc-stack-1", "shop_web", "shop"))
	srv := newResourceTestServer(t, c)

	name := t.Name()
	srv.notifications.Subscribe(session(name), "cetacean://stacks/shop", nil)

	c.SetService(stackService("svc-stack-1", "shop_web", "shop"))
	assertUpdated(t, drainUpdated(t, name), "cetacean://stacks/shop")

	c.SetTask(swarm.Task{ID: "task-stack-1", ServiceID: "svc-stack-1"})
	assertUpdated(t, drainUpdated(t, name), "cetacean://stacks/shop")

	c.SetService(stackService("svc-stack-2", "blog_web", "blog"))
	assertUpdated(t, drainUpdated(t, name))
}

func TestSubscriptionByNameResolvesToID(t *testing.T) {
	c := cache.New(nil)
	c.SetService(stackService("svc-byname", "web_api", ""))
	srv := newResourceTestServer(t, c)

	name := t.Name()
	srv.notifications.Subscribe(session(name), "cetacean://services/web_api", nil)

	c.SetService(stackService("svc-byname", "web_api", ""))
	assertUpdated(t, drainUpdated(t, name), "cetacean://services/web_api")
}

func TestSubscriptionUnreadableNameStaysUnresolved(t *testing.T) {
	c := cache.New(nil)
	c.SetService(stackService("svc-hidden", "hidden", ""))

	e := acl.NewEvaluator()
	e.SetResolver(c)
	e.SetPolicy(readOnlyPolicy("service:public-*"))
	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	got := srv.subscriptionFor("cetacean://services/hidden", &auth.Identity{Subject: "alice"})
	if got.target.id != "hidden" {
		t.Fatalf("target id = %q, want the name as written", got.target.id)
	}
}

func TestSubscriptionServiceAndNodeFollowTheirTasks(t *testing.T) {
	c := cache.New(nil)
	c.SetService(stackService("svc-tasks", "worker", ""))
	c.SetNode(swarm.Node{ID: "node-tasks", Description: swarm.NodeDescription{Hostname: "n1"}})
	srv := newResourceTestServer(t, c)

	name := t.Name()
	srv.notifications.Subscribe(session(name), "cetacean://services/worker", nil)
	srv.notifications.Subscribe(session(name), "cetacean://nodes/n1", nil)

	c.SetTask(swarm.Task{ID: "task-follow", ServiceID: "svc-tasks", NodeID: "node-tasks"})

	got := drainUpdated(t, name)
	if len(got) != 2 {
		t.Fatalf("updated = %v, want the service and the node", got)
	}
}

func TestSubscriptionLogsFollowServiceTasks(t *testing.T) {
	c := cache.New(nil)
	c.SetService(stackService("svc-logs", "logger", ""))
	srv := newResourceTestServer(t, c)

	name := t.Name()
	srv.notifications.Subscribe(session(name), "cetacean://services/svc-logs/logs", nil)

	c.SetTask(swarm.Task{ID: "task-logs", ServiceID: "svc-logs"})
	assertUpdated(t, drainUpdated(t, name), "cetacean://services/svc-logs/logs")
}

func TestSubscriptionResyncReachesReadableSubscribers(t *testing.T) {
	c := cache.New(nil)
	c.SetService(stackService("svc-pub", "public-api", ""))
	c.SetService(stackService("svc-priv", "private-api", ""))

	e := acl.NewEvaluator()
	e.SetResolver(c)
	e.SetPolicy(readOnlyPolicy("service:public-*"))
	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	id := &auth.Identity{Subject: "alice"}
	name := t.Name()
	srv.notifications.Subscribe(session(name), "cetacean://services/svc-pub", id)
	srv.notifications.Subscribe(session(name), "cetacean://services/svc-priv", id)
	srv.notifications.Subscribe(session(name), "cetacean://cluster", id)

	srv.dispatchCacheEvent(cache.Event{Type: cache.EventSync, Action: "full_sync"})
	assertUpdated(t, drainUpdated(t, name), "cetacean://services/svc-pub")
}

// The overview is not subscribable, and says so rather than staying silent.
func TestClusterResourceDoesNotPromisePush(t *testing.T) {
	for _, r := range staticResources {
		if r.uri == "cetacean://cluster" && strings.Contains(r.description, "updates push") {
			t.Fatalf("cluster description promises push: %q", r.description)
		}
	}
}
