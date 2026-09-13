package api

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

func topologyCache(n int) *cache.Cache {
	c := cache.New(nil)
	for i := range n {
		id := fmt.Sprintf("id-%d", i)
		c.SetNetwork(
			network.Summary{
				ID:     id,
				Name:   fmt.Sprintf("net-%d", i),
				Driver: "overlay",
				Scope:  "swarm",
			},
		)
		c.SetService(swarm.Service{
			ID: id,
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: fmt.Sprintf("svc-%d", i)},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{Image: "img"},
					Networks:      []swarm.NetworkAttachmentConfig{{Target: id}},
				},
			},
		})
		c.SetNode(
			swarm.Node{
				ID:          id,
				Description: swarm.NodeDescription{Hostname: fmt.Sprintf("n-%d", i)},
			},
		)
	}
	return c
}

func getTopology(t *testing.T, h *Handlers, id *auth.Identity) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/topology", nil)
	if id != nil {
		req = req.WithContext(auth.ContextWithIdentity(req.Context(), id))
	}
	w := httptest.NewRecorder()
	h.HandleTopology(w, req)
	if w.Code != 200 {
		t.Fatalf("topology returned %d: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

// A hit must be indistinguishable from a build.
func TestTopologyMemoMatchesFreshBuild(t *testing.T) {
	c := topologyCache(20)
	warm := newTestHandlers(t, withCache(c))
	cold := newTestHandlers(t, withCache(c))

	first := getTopology(t, warm, nil)
	second := getTopology(t, warm, nil) // served from the memo
	fresh := getTopology(t, cold, nil)  // built again, different Handlers

	if first != second {
		t.Error("a memoised topology differs from the response that populated it")
	}
	if second != fresh {
		t.Error("a memoised topology differs from one built from scratch")
	}
}

// The property that matters: two identities that may see different things must
// never be handed each other's document.
func TestTopologyMemoSeparatesIdentities(t *testing.T) {
	c := topologyCache(6)

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:*", "node:*"},
			Permissions: []string{"read"},
			Audience:    []string{"group:all"},
		},
		{
			Resources:   []string{"service:svc-0", "node:*"},
			Permissions: []string{"read"},
			Audience:    []string{"group:one"},
		},
	}})
	h := newTestHandlers(t, withCache(c), withACL(e))

	broad := &auth.Identity{Subject: "b", Groups: []string{"all"}}
	narrow := &auth.Identity{Subject: "n", Groups: []string{"one"}}

	broadDoc := getTopology(t, h, broad)
	narrowDoc := getTopology(t, h, narrow)

	if broadDoc == narrowDoc {
		t.Fatal("identities with different grants received the same topology")
	}

	// And again, now that both are memoised, in the other order.
	if got := getTopology(t, h, narrow); got != narrowDoc {
		t.Error("the narrow identity was served a different document on a memo hit")
	}
	if got := getTopology(t, h, broad); got != broadDoc {
		t.Error("the broad identity was served a different document on a memo hit")
	}
}

func TestTopologyMemoInvalidatesOnMutation(t *testing.T) {
	c := topologyCache(4)
	h := newTestHandlers(t, withCache(c))

	before := getTopology(t, h, nil)

	c.SetService(swarm.Service{
		ID: "added",
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "svc-added"},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "img"}},
		},
	})

	if after := getTopology(t, h, nil); after == before {
		t.Error("adding a service did not change the topology; the memo served a stale document")
	}
}

// A full resync replaces everything behind a single sync event. This is the
// case a history-based generation would have missed.
func TestTopologyMemoInvalidatesOnResync(t *testing.T) {
	c := topologyCache(4)
	h := newTestHandlers(t, withCache(c))

	before := getTopology(t, h, nil)

	c.ReplaceAll(cache.FullSyncData{
		HasServices: true,
		Services: []swarm.Service{{
			ID: "only",
			Spec: swarm.ServiceSpec{
				Annotations:  swarm.Annotations{Name: "svc-only"},
				TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "img"}},
			},
		}},
	})

	if after := getTopology(t, h, nil); after == before {
		t.Error("a full resync did not change the topology; the memo served a stale document")
	}
}

func TestProjectionCacheEvictsOldest(t *testing.T) {
	p := newProjectionCache()
	for i := range projectionCacheSize + 4 {
		p.put(projectionKey{generation: uint64(i)}, []byte{byte(i)})
	}

	if _, ok := p.get(projectionKey{generation: 0}); ok {
		t.Error("the oldest entry survived past the size bound")
	}
	newest := projectionKey{generation: uint64(projectionCacheSize + 3)}
	if _, ok := p.get(newest); !ok {
		t.Error("the newest entry was evicted")
	}
	if len(p.entries) > projectionCacheSize {
		t.Errorf("cache holds %d entries, bound is %d", len(p.entries), projectionCacheSize)
	}
}
