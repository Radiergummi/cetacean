package mcp

import (
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
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

// A name the caller cannot read delivers nothing, and starts delivering once a
// policy change makes it readable.
func TestSubscriptionByNameFollowsReadability(t *testing.T) {
	c := cache.New(nil)
	c.SetService(stackService("svc-hidden", "hidden", ""))

	e := acl.NewEvaluator()
	e.SetResolver(c)
	e.SetPolicy(readOnlyPolicy("service:public-*"))
	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	name := t.Name()
	srv.notifications.Subscribe(
		session(name),
		"cetacean://services/hidden",
		&auth.Identity{Subject: "alice"},
	)

	c.SetService(stackService("svc-hidden", "hidden", ""))
	assertUpdated(t, drainUpdated(t, name))

	e.SetPolicy(readOnlyPolicy("service:*"))
	c.SetService(stackService("svc-hidden", "hidden", ""))
	assertUpdated(t, drainUpdated(t, name), "cetacean://services/hidden")
}

// A name keeps naming whatever holds it: subscribing ahead of a service, and
// a service removed and created again under a new ID.
func TestSubscriptionByNameFollowsRecreation(t *testing.T) {
	c := cache.New(nil)
	srv := newResourceTestServer(t, c)

	name := t.Name()
	srv.notifications.Subscribe(session(name), "cetacean://services/web", nil)
	srv.notifications.Subscribe(session(name), "cetacean://stacks/shop", nil)

	c.SetService(stackService("svc-first", "web", "shop"))
	got := drainUpdated(t, name)
	slices.Sort(got)
	assertUpdated(t, got, "cetacean://services/web", "cetacean://stacks/shop")

	c.DeleteService("svc-first")
	drainUpdated(t, name)

	c.SetService(stackService("svc-second", "web", "shop"))
	drainUpdated(t, name)

	c.SetTask(swarm.Task{ID: "task-second", ServiceID: "svc-second"})
	got = drainUpdated(t, name)
	slices.Sort(got)
	assertUpdated(t, got, "cetacean://services/web", "cetacean://stacks/shop")
}

// A node that rejoins comes back under a new ID and the same hostname.
func TestSubscriptionByHostnameFollowsARejoinedNode(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node-old", Description: swarm.NodeDescription{Hostname: "n1"}})
	srv := newResourceTestServer(t, c)

	name := t.Name()
	srv.notifications.Subscribe(session(name), "cetacean://nodes/n1", nil)

	c.DeleteNode("node-old")
	drainUpdated(t, name)

	c.SetNode(swarm.Node{ID: "node-new", Description: swarm.NodeDescription{Hostname: "n1"}})
	assertUpdated(t, drainUpdated(t, name), "cetacean://nodes/n1")
}

// "<service>.<slot>" names whichever replica holds the slot, so the replica
// that replaces a failed one is the one a subscription follows.
func TestSubscriptionBySlotFollowsTheReplacementReplica(t *testing.T) {
	c := slotCache()
	srv := newResourceTestServer(t, c)

	name := t.Name()
	srv.notifications.Subscribe(session(name), "cetacean://tasks/web.1", nil)

	c.SetTask(swarm.Task{
		ID: "task-live-1", ServiceID: "svc1", Slot: 1,
		DesiredState: swarm.TaskStateShutdown,
		Status:       swarm.TaskStatus{State: swarm.TaskStateFailed},
	})
	drainUpdated(t, name)

	c.SetTask(swarm.Task{
		ID: "task-live-2", ServiceID: "svc1", Slot: 1,
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateStarting},
	})
	assertUpdated(t, drainUpdated(t, name), "cetacean://tasks/web.1")

	c.SetTask(swarm.Task{
		ID: "task-live-2", ServiceID: "svc1", Slot: 1,
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	})
	assertUpdated(t, drainUpdated(t, name), "cetacean://tasks/web.1")

	c.SetTask(swarm.Task{ID: "task-other", ServiceID: "svc1", Slot: 2})
	assertUpdated(t, drainUpdated(t, name))
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

func TestSubscriptionNetworkFollowsAttachedTasks(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net-follow", Name: "backend", Scope: "swarm"})
	srv := newResourceTestServer(t, c)

	name := t.Name()
	srv.notifications.Subscribe(session(name), "cetacean://networks/backend", nil)

	c.SetTask(swarm.Task{ID: "task-elsewhere"})
	c.SetTask(swarm.Task{
		ID: "task-attached",
		NetworksAttachments: []swarm.NetworkAttachment{{
			Network: swarm.Network{ID: "net-follow"},
		}},
	})

	assertUpdated(t, drainUpdated(t, name), "cetacean://networks/backend")
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

// A task reaches its service's subscribers only where the service itself is
// readable, or the timing of its tasks would confirm a guessed name.
func TestSubscriptionParentMustBeReadable(t *testing.T) {
	c := cache.New(nil)
	c.SetService(stackService("svc-hidden", "hidden", "shop"))

	e := acl.NewEvaluator()
	e.SetResolver(c)
	e.SetPolicy(readOnlyPolicy("task:*"))
	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	id := &auth.Identity{Subject: "alice"}
	name := t.Name()
	srv.notifications.Subscribe(session(name), "cetacean://services/hidden", id)
	srv.notifications.Subscribe(session(name), "cetacean://stacks/shop", id)
	srv.notifications.Subscribe(session(name), "cetacean://tasks/task-hidden", id)

	c.SetTask(swarm.Task{ID: "task-hidden", ServiceID: "svc-hidden"})
	assertUpdated(t, drainUpdated(t, name), "cetacean://tasks/task-hidden")
}
