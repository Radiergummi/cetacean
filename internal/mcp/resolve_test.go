package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// A cetacean:// read addresses a resource by the identifier a caller has in
// hand, and after find and describe that identifier is a name far more often
// than an ID — every prompt tells an agent to resolve the name first, and
// completions offer names because a dropdown of hex IDs is unusable. Six of
// the eight types are keyed by ID in the cache, so a name reached none of
// them until lookupResource learned to fall back.
func TestLookupResolvesNamesAsWellAsIDs(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node-abc123",
		Description: swarm.NodeDescription{Hostname: "styx"},
	})
	c.SetService(swarm.Service{
		ID:   "svc-abc123",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetConfig(swarm.Config{
		ID:   "cfg-abc123",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app-config"}},
	})
	c.SetSecret(swarm.Secret{
		ID:   "sec-abc123",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "api-token"}},
	})
	c.SetNetwork(network.Summary{ID: "net-abc123", Name: "backend"})

	srv := newResourceTestServer(t, c)

	cases := []struct {
		uri  string
		want string
	}{
		{"cetacean://nodes/styx", "node-abc123"},
		{"cetacean://services/web", "svc-abc123"},
		{"cetacean://configs/app-config", "cfg-abc123"},
		{"cetacean://secrets/api-token", "sec-abc123"},
		{"cetacean://networks/backend", "net-abc123"},
	}

	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			body, err := srv.readResource(context.Background(), tc.uri)
			if err != nil {
				t.Fatalf("readResource(%q): %v", tc.uri, err)
			}

			if !strings.Contains(body, tc.want) {
				t.Errorf("readResource(%q) = %s, want it to name %q", tc.uri, body, tc.want)
			}
		})
	}
}

// An ID must keep winning outright. A name lookup is the fallback, so a
// resource whose name happens to be another's ID cannot shadow it.
func TestLookupPrefersIDOverName(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "collide",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "real-id-holder"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc-other",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "collide"}},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://services/collide")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	if !strings.Contains(body, "real-id-holder") {
		t.Errorf("got %s, want the service whose ID is \"collide\"", body)
	}
}

// Node hostnames are not unique — Swarm does not enforce it, and two hosts
// installed from one image routinely share one. Picking either silently would
// describe the wrong machine, which is worse on a read a human acts on than
// being told to use an ID.
func TestLookupRejectsAnAmbiguousName(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node-1", Description: swarm.NodeDescription{Hostname: "worker"}})
	c.SetNode(swarm.Node{ID: "node-2", Description: swarm.NodeDescription{Hostname: "worker"}})

	srv := newResourceTestServer(t, c)

	_, err := srv.readResource(context.Background(), "cetacean://nodes/worker")
	if err == nil {
		t.Fatal("readResource: want an ambiguity error, got nil")
	}

	for _, want := range []string{"worker", "node-1", "node-2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// A name that matches nothing must stay a not-found, not become a different
// error class — the read path deliberately makes "denied" and "absent"
// indistinguishable, and a name miss must not open a way to tell them apart.
func TestLookupUnknownNameIsStillNotFound(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	_, err := srv.readResource(context.Background(), "cetacean://services/nope")
	if err == nil {
		t.Fatal("readResource: want an error, got nil")
	}

	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q does not name the resource asked for", err)
	}
}

// A task is rendered "<service>.<slot>" everywhere a caller sees one, so that
// is the identifier they will paste back — and the one a completion offers.
func TestLookupResolvesATaskByItsRenderedName(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc-api",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api"}},
	})
	c.SetTask(swarm.Task{ID: "task-xyz", ServiceID: "svc-api", Slot: 2})

	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://tasks/api.2")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	if !strings.Contains(body, "task-xyz") {
		t.Errorf("got %s, want it to name task-xyz", body)
	}
}
