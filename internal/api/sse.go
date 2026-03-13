package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/cache"
)

type sseClient struct {
	events chan cache.Event
	match  func(cache.Event) bool // nil means accept all
	done   chan struct{}
}

const maxSSEClients = 256

type Broadcaster struct {
	mu            sync.RWMutex
	clients       map[*sseClient]struct{}
	closed        bool
	inbox         chan cache.Event
	stop          chan struct{}
	batchInterval time.Duration
}

func NewBroadcaster(batchInterval time.Duration) *Broadcaster {
	if batchInterval <= 0 {
		batchInterval = 100 * time.Millisecond
	}
	b := &Broadcaster{
		clients:       make(map[*sseClient]struct{}),
		inbox:         make(chan cache.Event, 256),
		stop:          make(chan struct{}),
		batchInterval: batchInterval,
	}
	go b.fanOut()
	return b
}

// Broadcast enqueues an event for delivery to SSE clients.
// Non-blocking: drops the event if the internal buffer is full.
func (b *Broadcaster) Broadcast(e cache.Event) {
	select {
	case b.inbox <- e:
	default:
		slog.Warn("SSE broadcast buffer full, dropping event", "type", e.Type, "id", e.ID)
	}
}

// fanOut is the dedicated goroutine that delivers events to SSE clients.
func (b *Broadcaster) fanOut() {
	for {
		select {
		case e := <-b.inbox:
			b.mu.RLock()
			for c := range b.clients {
				if c.match != nil && !c.match(e) {
					continue
				}
				select {
				case c.events <- e:
				default:
					// Slow client, drop event
				}
			}
			b.mu.RUnlock()
		case <-b.stop:
			return
		}
	}
}

func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.stop)
	for c := range b.clients {
		close(c.done)
	}
}

func (b *Broadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var match func(cache.Event) bool
	if t := r.URL.Query().Get("types"); t != "" {
		types := make(map[string]bool)
		for typ := range strings.SplitSeq(t, ",") {
			types[strings.TrimSpace(typ)] = true
		}
		match = func(e cache.Event) bool { return types[e.Type] }
	}
	b.serveSSE(w, r, match)
}

func (b *Broadcaster) serveSSE(w http.ResponseWriter, r *http.Request, match func(cache.Event) bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "streaming not supported")
		return
	}

	client := &sseClient{
		events: make(chan cache.Event, 64),
		match:  match,
		done:   make(chan struct{}),
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	if len(b.clients) >= maxSSEClients {
		b.mu.Unlock()
		w.Header().Set("Retry-After", "5")
		writeProblem(w, r, http.StatusTooManyRequests, "too many SSE connections")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	b.clients[client] = struct{}{}
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, client)
		b.mu.Unlock()
	}()

	var eventID uint64
	batchTicker := time.NewTicker(b.batchInterval)
	defer batchTicker.Stop()
	var batch []cache.Event

	for {
		select {
		case e, ok := <-client.events:
			if !ok {
				if len(batch) > 0 {
					writeBatch(w, flusher, batch, &eventID)
				}
				return
			}
			batch = append(batch, e)
		case <-batchTicker.C:
			if len(batch) > 0 {
				writeBatch(w, flusher, batch, &eventID)
				batch = batch[:0]
			}
		case <-r.Context().Done():
			if len(batch) > 0 {
				writeBatch(w, flusher, batch, &eventID)
			}
			return
		case <-client.done:
			if len(batch) > 0 {
				writeBatch(w, flusher, batch, &eventID)
			}
			return
		}
	}
}

// typeMatcher returns a match function that accepts events of the given type.
// Sync events always pass through so clients can trigger a full refetch.
func typeMatcher(typ string) func(cache.Event) bool {
	return func(e cache.Event) bool {
		return e.Type == typ || e.Type == "sync"
	}
}

// resourceMatcher returns a match function for per-resource SSE streams.
// Sync events always pass through so clients can trigger a full refetch.
func resourceMatcher(typ, id string) func(cache.Event) bool {
	switch typ {
	case "node":
		return func(e cache.Event) bool {
			if e.Type == "sync" {
				return true
			}
			if e.Type == "node" && e.ID == id {
				return true
			}
			if e.Type == "task" {
				if t, ok := e.Resource.(swarm.Task); ok {
					return t.NodeID == id
				}
			}
			return false
		}
	case "service":
		return func(e cache.Event) bool {
			if e.Type == "sync" {
				return true
			}
			if e.Type == "service" && e.ID == id {
				return true
			}
			if e.Type == "task" {
				if t, ok := e.Resource.(swarm.Task); ok {
					return t.ServiceID == id
				}
			}
			return false
		}
	case "task":
		return func(e cache.Event) bool {
			return e.Type == "sync" || (e.Type == "task" && e.ID == id)
		}
	default:
		// config, secret, network, volume — match by type+ID.
		// Cross-reference "ref_changed" events already have the correct type+ID.
		return func(e cache.Event) bool {
			return e.Type == "sync" || (e.Type == typ && e.ID == id)
		}
	}
}

// stackMatcher returns a match function for stack SSE streams.
// Sync events always pass through so clients can trigger a full refetch.
func stackMatcher(c *cache.Cache, name string) func(cache.Event) bool {
	return func(e cache.Event) bool {
		if e.Type == "sync" {
			return true
		}
		stack, ok := c.GetStack(name)
		if !ok {
			return false
		}
		switch e.Type {
		case "service":
			return slices.Contains(stack.Services, e.ID)
		case "config":
			return slices.Contains(stack.Configs, e.ID)
		case "secret":
			return slices.Contains(stack.Secrets, e.ID)
		case "network":
			return slices.Contains(stack.Networks, e.ID)
		case "volume":
			return slices.Contains(stack.Volumes, e.ID)
		case "task":
			if t, ok := e.Resource.(swarm.Task); ok {
				return slices.Contains(stack.Services, t.ServiceID)
			}
			return false
		case "stack":
			return e.ID == name
		default:
			return false
		}
	}
}

// sseEvent is the JSON-LD enriched wire format for SSE event payloads.
type sseEvent struct {
	AtID     string `json:"@id,omitempty"`
	AtType   string `json:"@type,omitempty"`
	Type     string `json:"type"`
	Action   string `json:"action"`
	ID       string `json:"id"`
	Resource any    `json:"resource,omitempty"`
}

func toSSEEvent(e cache.Event) sseEvent {
	return sseEvent{
		AtID:     resourcePath(e.Type, e.ID),
		AtType:   resourceType(e.Type),
		Type:     e.Type,
		Action:   e.Action,
		ID:       e.ID,
		Resource: e.Resource,
	}
}

// resourcePath returns the canonical API path for a resource.
func resourcePath(typ, id string) string {
	switch typ {
	case "node":
		return "/nodes/" + id
	case "service":
		return "/services/" + id
	case "task":
		return "/tasks/" + id
	case "config":
		return "/configs/" + id
	case "secret":
		return "/secrets/" + id
	case "network":
		return "/networks/" + id
	case "volume":
		return "/volumes/" + id
	case "stack":
		return "/stacks/" + id
	default:
		return ""
	}
}

// resourceType returns the JSON-LD @type for a resource type string.
func resourceType(typ string) string {
	switch typ {
	case "node":
		return "Node"
	case "service":
		return "Service"
	case "task":
		return "Task"
	case "config":
		return "Config"
	case "secret":
		return "Secret"
	case "network":
		return "Network"
	case "volume":
		return "Volume"
	case "stack":
		return "Stack"
	default:
		return ""
	}
}

func writeBatch(w io.Writer, flusher http.Flusher, events []cache.Event, eventID *uint64) {
	*eventID++
	if len(events) == 1 {
		data, _ := json.Marshal(toSSEEvent(events[0]))
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", *eventID, events[0].Type, data)
	} else {
		enriched := make([]sseEvent, len(events))
		for i, e := range events {
			enriched[i] = toSSEEvent(e)
		}
		data, _ := json.Marshal(enriched)
		fmt.Fprintf(w, "id: %d\nevent: batch\ndata: %s\n\n", *eventID, data)
	}
	flusher.Flush()
}
