package cache

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

var alice = Actor{Subject: "u-1", Name: "alice", Provider: "oidc", Transport: TransportMCP}

func serviceAt(id string, index uint64) swarm.Service {
	return swarm.Service{
		ID:      id,
		Version: swarm.Version{Index: index},
		Spec:    swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	}
}

func newestEntry(t *testing.T, c *Cache) HistoryEntry {
	t.Helper()

	entries := c.History().List(HistoryQuery{Limit: 1})
	if len(entries) != 1 {
		t.Fatalf("history holds %d entries, want at least one", len(entries))
	}

	return entries[0]
}

func TestAttributionMatchesTheVersionTheWriteProduced(t *testing.T) {
	c := New(nil)
	c.SetService(serviceAt("s1", 10))

	c.Attributions().Expect(Expectation{Type: EventService, Key: "s1", After: 10}, alice)

	// A replay of the version the write was made against is not the write.
	c.SetService(serviceAt("s1", 10))
	if got := newestEntry(t, c); got.Actor != nil {
		t.Fatalf("an event at the written version was attributed to %+v", got.Actor)
	}

	c.SetService(serviceAt("s1", 11))
	got := newestEntry(t, c)
	if got.Actor == nil || *got.Actor != alice {
		t.Fatalf("actor = %+v, want %+v", got.Actor, alice)
	}

	if n := c.Attributions().Len(); n != 0 {
		t.Errorf("%d attributions still pending after the match", n)
	}
}

func TestUnmatchedChangesStayAnonymous(t *testing.T) {
	c := New(nil)
	c.SetService(serviceAt("s1", 10))
	c.SetService(serviceAt("other", 3))

	c.Attributions().Expect(Expectation{Type: EventService, Key: "s1", After: 10}, alice)

	c.SetService(serviceAt("other", 4))
	if got := newestEntry(t, c); got.Actor != nil {
		t.Fatalf("a change to another service was attributed to %+v", got.Actor)
	}

	c.SetService(serviceAt("s1", 11))
	c.SetService(serviceAt("s1", 12))
	if got := newestEntry(t, c); got.Actor != nil {
		t.Fatalf("the change after the attributed one was attributed to %+v", got.Actor)
	}
}

func TestTheNewestPendingWriteWinsAndOlderOnesGo(t *testing.T) {
	c := New(nil)
	c.SetService(serviceAt("s1", 10))

	bob := alice
	bob.Name = "bob"

	c.Attributions().Expect(Expectation{Type: EventService, Key: "s1", After: 10}, alice)
	c.Attributions().Expect(Expectation{Type: EventService, Key: "s1", After: 12}, bob)

	// The watcher coalesced both writes into one inspect.
	c.SetService(serviceAt("s1", 14))

	got := newestEntry(t, c)
	if got.Actor == nil || got.Actor.Name != "bob" {
		t.Fatalf("actor = %+v, want bob", got.Actor)
	}

	c.SetService(serviceAt("s1", 16))
	if got := newestEntry(t, c); got.Actor != nil {
		t.Fatalf("the superseded write matched a later change: %+v", got.Actor)
	}
}

func TestAttributionMatchesCreatesByNameAndRemovesByKey(t *testing.T) {
	c := New(nil)

	c.Attributions().
		Expect(Expectation{Type: EventConfig, Key: "app.conf", Action: "create"}, alice)
	c.SetConfig(swarm.Config{ID: "c1", Spec: swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: "app.conf"},
	}})

	if got := newestEntry(t, c); got.Action != "create" || got.Actor == nil {
		t.Fatalf("create entry = %+v, want it attributed", got)
	}

	c.SetVolume(volume.Volume{Name: "data"})
	c.Attributions().Expect(Expectation{Type: EventVolume, Key: "data", Action: "remove"}, alice)
	c.DeleteVolume("data")

	if got := newestEntry(t, c); got.Action != "remove" || got.Actor == nil {
		t.Fatalf("remove entry = %+v, want it attributed", got)
	}
}

func TestWithdrawnAttributionMatchesNothing(t *testing.T) {
	c := New(nil)
	c.SetService(serviceAt("s1", 10))

	withdraw := c.Attributions().
		Expect(Expectation{Type: EventService, Key: "s1", After: 10}, alice)
	withdraw()

	c.SetService(serviceAt("s1", 11))
	if got := newestEntry(t, c); got.Actor != nil {
		t.Fatalf("a withdrawn write was attributed: %+v", got.Actor)
	}
}

func TestPendingAttributionsExpire(t *testing.T) {
	c := New(nil)
	c.SetService(serviceAt("s1", 10))

	now := time.Now()
	c.Attributions().now = func() time.Time { return now }
	c.Attributions().Expect(Expectation{Type: EventService, Key: "s1", After: 10}, alice)

	now = now.Add(attributionTTL)

	if n := c.Attributions().Len(); n != 0 {
		t.Fatalf("%d attributions pending past their lifetime", n)
	}

	c.SetService(serviceAt("s1", 11))
	if got := newestEntry(t, c); got.Actor != nil {
		t.Fatalf("an expired write was attributed: %+v", got.Actor)
	}
}

func TestPendingAttributionsAreBounded(t *testing.T) {
	a := newAttributions()

	for range maxAttributions + 10 {
		a.Expect(Expectation{Type: EventService, Key: "s1", After: 1}, alice)
	}

	if n := a.Len(); n != maxAttributions {
		t.Fatalf("%d attributions pending, want the bound %d", n, maxAttributions)
	}
}

func TestActorTravelsInTheContext(t *testing.T) {
	if _, ok := ActorFromContext(context.Background()); ok {
		t.Fatal("an empty context carries an actor")
	}

	got, ok := ActorFromContext(WithActor(context.Background(), alice))
	if !ok || got != alice {
		t.Fatalf("actor = %+v, %v; want %+v", got, ok, alice)
	}
}

// An entry recorded before attribution existed, or for a change made
// elsewhere, decodes with no actor rather than an empty one.
func TestHistoryEntryWithoutActorDecodes(t *testing.T) {
	var old HistoryEntry
	if err := json.Unmarshal(
		[]byte(`{"id":1,"timestamp":"2026-10-25T10:00:00Z","type":"service","action":"update"}`),
		&old,
	); err != nil {
		t.Fatal(err)
	}

	if old.Actor != nil {
		t.Fatalf("actor = %+v, want none", old.Actor)
	}

	attributed := HistoryEntry{ID: 2, Type: EventService, Action: "update", Actor: &alice}

	data, err := json.Marshal(attributed)
	if err != nil {
		t.Fatal(err)
	}

	var back HistoryEntry
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}

	if back.Actor == nil || *back.Actor != alice {
		t.Fatalf("round-tripped actor = %+v, want %+v", back.Actor, alice)
	}
}
