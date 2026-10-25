package mcp

import (
	"context"
	"net/http"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// A write over MCP carries the caller as its actor, including the OAuth
// client its token was minted for.
func TestWritesOverMCPCarryTheirActor(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "web", 1, 1)

	var (
		actor    cache.Actor
		recorded bool
	)

	writeClient := &fakeWriteClient{
		scaleServiceFn: func(ctx context.Context, id string, _ uint64) (swarm.Service, error) {
			actor, recorded = cache.ActorFromContext(ctx)
			return swarm.Service{ID: id}, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsOperational).Handler()
	identity := &auth.Identity{
		Subject:     "u-1",
		DisplayName: "alice",
		Provider:    auth.ProviderToken,
		ClientID:    "claude",
	}
	authenticated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), identity)))
	})

	_, envelope := mcpModern(t, authenticated, 1, "tools/call",
		`{"name":"scale_service","arguments":{"id":"web","replicas":1}}`)
	if envelope.Error != nil {
		t.Fatalf("scale_service failed: %+v", envelope.Error)
	}

	if !recorded {
		t.Fatal("the write reached the writer with no actor")
	}

	want := cache.Actor{
		Subject:   "u-1",
		Name:      "alice",
		Provider:  auth.ProviderToken,
		Client:    "claude",
		Transport: cache.TransportMCP,
	}
	if actor != want {
		t.Errorf("actor = %+v, want %+v", actor, want)
	}
}

// get_events carries the actor of a change made through Cetacean, and none
// for one made elsewhere.
func TestGetEventsCarriesTheActor(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "other",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "other"}},
	})

	alice := cache.Actor{Subject: "u-1", Name: "alice", Provider: "oidc", Transport: "dashboard"}
	c.Attributions().Expect(
		cache.Expectation{Type: cache.EventService, Key: "web", Action: "create"},
		alice,
	)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	got := eventsCall(t, newResourceTestServer(t, c), map[string]any{})

	actors := map[string]*cache.Actor{}
	for _, entry := range got.Entries {
		actors[entry.Name] = entry.Actor
	}

	if a := actors["web"]; a == nil || *a != alice {
		t.Errorf("web's actor = %+v, want %+v", a, alice)
	}
	if a := actors["other"]; a != nil {
		t.Errorf("the external change carries an actor: %+v", a)
	}
}
