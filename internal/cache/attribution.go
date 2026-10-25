package cache

import (
	"context"
	"sync"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

// Transport is how a write reached Cetacean.
type Transport string

const (
	TransportDashboard Transport = "dashboard"
	TransportREST      Transport = "rest"
	TransportMCP       Transport = "mcp"
)

// Actor is who made a change through Cetacean, as the history records it.
type Actor struct {
	Subject   string    `json:"subject"`
	Name      string    `json:"name,omitempty"`
	Provider  string    `json:"provider"`
	Client    string    `json:"client,omitempty"`
	Transport Transport `json:"transport"`
}

// DisplayName is the name a reader knows the actor by, the subject when the
// provider sent no other.
func (a Actor) DisplayName() string {
	if a.Name != "" {
		return a.Name
	}

	return a.Subject
}

type actorKey struct{}

// WithActor marks every write made under ctx as the actor's.
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFromContext returns the actor WithActor stored, if any.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorKey{}).(Actor)
	return actor, ok
}

// Expectation describes the event a write is about to cause. Action is
// "create" (Key is the name), "remove", or empty for a change to an existing
// resource, which matches only a version above After.
type Expectation struct {
	Type   EventType
	Key    string
	Action string
	After  uint64
}

const (
	attributionTTL   = time.Minute
	maxAttributions  = 1024
	actionCreate     = "create"
	actionRemove     = "remove"
	actionRefChanged = "ref_changed"
)

type pendingAttribution struct {
	Expectation
	actor   Actor
	expires time.Time
	seq     uint64
}

// Attributions holds the writes Cetacean made whose events have not arrived
// yet, so the history entry those events produce can name who made them.
type Attributions struct {
	mu      sync.Mutex
	pending []pendingAttribution
	seq     uint64
	now     func() time.Time
}

func newAttributions() *Attributions {
	return &Attributions{now: time.Now}
}

// Expect registers a write about to be made. It must be called before the
// write reaches the engine, or its event can arrive first. The returned
// function withdraws it, for a write that failed.
func (a *Attributions) Expect(x Expectation, actor Actor) (withdraw func()) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	a.prune(now)

	if len(a.pending) >= maxAttributions {
		a.pending = a.pending[1:]
	}

	a.seq++
	seq := a.seq
	a.pending = append(a.pending, pendingAttribution{
		Expectation: x,
		actor:       actor,
		expires:     now.Add(attributionTTL),
		seq:         seq,
	})

	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()

		for i, p := range a.pending {
			if p.seq == seq {
				a.pending = append(a.pending[:i], a.pending[i+1:]...)
				return
			}
		}
	}
}

// Len is the number of writes still waiting for their event.
func (a *Attributions) Len() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.prune(a.now())

	return len(a.pending)
}

// prune drops what has expired. Entries are appended in expiry order.
func (a *Attributions) prune(now time.Time) {
	i := 0
	for i < len(a.pending) && !now.Before(a.pending[i].expires) {
		i++
	}

	a.pending = a.pending[i:]
}

// match returns the actor of the write e is the result of, consuming it. A
// change consumes every older write to the same resource too: once the
// resource has moved past their version they could only match a later one.
func (a *Attributions) match(e Event) (Actor, bool) {
	if e.Action == actionRefChanged {
		return Actor{}, false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.prune(a.now())

	if len(a.pending) == 0 {
		return Actor{}, false
	}

	version, versioned := resourceVersion(e.Resource)
	best := -1

	for i, p := range a.pending {
		if p.Type != e.Type || !p.matches(e, version, versioned) {
			continue
		}

		if best < 0 || (p.Action == "" && p.After > a.pending[best].After) {
			best = i
		}
	}

	if best < 0 {
		return Actor{}, false
	}

	chosen := a.pending[best]
	kept := a.pending[:0]

	for i, p := range a.pending {
		consumed := i == best ||
			(chosen.Action == "" && p.Type == e.Type && p.matches(e, version, versioned))
		if !consumed {
			kept = append(kept, p)
		}
	}

	a.pending = kept

	return chosen.actor, true
}

func (p pendingAttribution) matches(e Event, version uint64, versioned bool) bool {
	switch p.Action {
	case actionCreate:
		return e.Action == actionCreate && e.Name == p.Key
	case actionRemove:
		return e.Action == actionRemove && e.ID == p.Key
	default:
		return e.Action != actionRemove && e.ID == p.Key && versioned && version > p.After
	}
}

// resourceVersion is the swarm object version an event carries, where it has
// one; networks and volumes are not versioned.
func resourceVersion(resource any) (uint64, bool) {
	switch r := resource.(type) {
	case swarm.Service:
		return r.Version.Index, true
	case swarm.Node:
		return r.Version.Index, true
	case swarm.Task:
		return r.Version.Index, true
	case swarm.Config:
		return r.Version.Index, true
	case swarm.Secret:
		return r.Version.Index, true
	default:
		return 0, false
	}
}
