package cache

import (
	"errors"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// The cache keys six of the eight resource types by ID, so a caller holding a
// name — which is what every listing shows and what a completion offers — had
// no way to reach one. Both transports resolve through here so they cannot
// disagree about what an identifier means.
func TestResolveFindsByIDAndByName(t *testing.T) {
	c := New(nil)
	c.SetService(swarm.Service{
		ID:   "svc-abc",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node-abc",
		Description: swarm.NodeDescription{Hostname: "styx"},
	})

	for _, identifier := range []string{"svc-abc", "web"} {
		svc, ok, err := c.ResolveService(identifier)
		if err != nil || !ok {
			t.Fatalf("ResolveService(%q) = ok %v, err %v", identifier, ok, err)
		}

		if svc.ID != "svc-abc" {
			t.Errorf("ResolveService(%q) = %q, want svc-abc", identifier, svc.ID)
		}
	}

	for _, identifier := range []string{"node-abc", "styx"} {
		node, ok, err := c.ResolveNode(identifier)
		if err != nil || !ok {
			t.Fatalf("ResolveNode(%q) = ok %v, err %v", identifier, ok, err)
		}

		if node.ID != "node-abc" {
			t.Errorf("ResolveNode(%q) = %q, want node-abc", identifier, node.ID)
		}
	}
}

// The ID lookup runs first, which is what settles the collision: a resource
// whose name happens to be another's ID must never shadow the ID holder.
func TestResolvePrefersIDOverName(t *testing.T) {
	c := New(nil)
	c.SetService(swarm.Service{
		ID:   "collide",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "real-id-holder"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc-other",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "collide"}},
	})

	svc, ok, err := c.ResolveService("collide")
	if err != nil || !ok {
		t.Fatalf("ResolveService = ok %v, err %v", ok, err)
	}

	if svc.Spec.Name != "real-id-holder" {
		t.Errorf("resolved %q, want the service whose ID is \"collide\"", svc.Spec.Name)
	}
}

// Swarm does not enforce unique node hostnames. Answering with either would
// describe the wrong machine, so the caller is told to disambiguate — and told
// which IDs to choose between.
func TestResolveReportsAnAmbiguousName(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{ID: "node-1", Description: swarm.NodeDescription{Hostname: "worker"}})
	c.SetNode(swarm.Node{ID: "node-2", Description: swarm.NodeDescription{Hostname: "worker"}})

	_, ok, err := c.ResolveNode("worker")
	if ok {
		t.Fatal("ResolveNode reported a match for an ambiguous hostname")
	}

	var ambiguous *AmbiguousNameError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("err = %v, want an *AmbiguousNameError", err)
	}

	if len(ambiguous.IDs) != 2 {
		t.Errorf("IDs = %v, want both candidates", ambiguous.IDs)
	}
}

// An identifier matching neither an ID nor a name is absent, not an error —
// the caller turns that into whatever "not found" means for its transport.
func TestResolveMissIsNotAnError(t *testing.T) {
	c := New(nil)

	_, ok, err := c.ResolveService("nope")
	if ok || err != nil {
		t.Errorf("ResolveService = ok %v, err %v, want absent and no error", ok, err)
	}
}
