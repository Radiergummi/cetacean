package sse

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/metrics"
)

// ReplaySource provides access to recent history entries for SSE replay.
type ReplaySource interface {
	Since(afterID uint64) ([]cache.HistoryEntry, bool)
	Count() uint64
}

type sseClient struct {
	events chan cache.Event
	match  func(cache.Event) bool // nil means accept all
	done   chan struct{}
}

const MaxClients = 256

// ErrorWriter is a callback for writing HTTP error responses.
// This decouples the SSE package from the API error registry.
type ErrorWriter func(w http.ResponseWriter, r *http.Request, code, detail string)

type Broadcaster struct {
	mu                sync.RWMutex
	clients           map[*sseClient]struct{}
	closed            bool
	inbox             chan cache.Event
	stop              chan struct{}
	batchInterval     time.Duration
	keepaliveInterval time.Duration
	writeError        ErrorWriter
	replay            ReplaySource
}

func NewBroadcaster(batchInterval time.Duration, writeError ErrorWriter, replay ReplaySource) *Broadcaster {
	if batchInterval <= 0 {
		batchInterval = 100 * time.Millisecond
	}
	b := &Broadcaster{
		clients:           make(map[*sseClient]struct{}),
		inbox:             make(chan cache.Event, 256),
		stop:              make(chan struct{}),
		batchInterval:     batchInterval,
		keepaliveInterval: 15 * time.Second,
		writeError:        writeError,
		replay:            replay,
	}
	go b.fanOut()
	return b
}

// Broadcast enqueues an event for delivery to SSE clients.
// Non-blocking: drops the event if the internal buffer is full.
func (b *Broadcaster) Broadcast(e cache.Event) {
	select {
	case b.inbox <- e:
		metrics.RecordSSEBroadcast()
	default:
		metrics.RecordSSEDrop()
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
		match = func(e cache.Event) bool { return types[string(e.Type)] }
	}
	b.ServeSSE(w, r, match, "")
}

func (b *Broadcaster) ServeSSE(
	w http.ResponseWriter,
	r *http.Request,
	match func(cache.Event) bool,
	replayType cache.EventType,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		b.writeError(w, r, "API005", "streaming not supported")
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
	if len(b.clients) >= MaxClients {
		b.mu.Unlock()
		w.Header().Set("Retry-After", "5")
		b.writeError(w, r, "SSE001", "too many SSE connections")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	b.clients[client] = struct{}{}
	b.mu.Unlock()
	metrics.RecordSSEConnect()

	flusher.Flush()

	var skipBelow uint64
	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" && b.replay != nil {
		if id, err := strconv.ParseUint(lastID, 10, 64); err == nil {
			skipBelow = b.replayEvents(w, flusher, id, replayType)
		}
	}

	defer func() {
		b.mu.Lock()
		delete(b.clients, client)
		// Close the done channel if it wasn't already closed by Broadcaster.Close(),
		// so any goroutine selecting on it can unblock.
		select {
		case <-client.done:
		default:
			close(client.done)
		}
		b.mu.Unlock()
		metrics.RecordSSEDisconnect()
	}()

	batchTicker := time.NewTicker(b.batchInterval)
	defer batchTicker.Stop()
	keepalive := time.NewTicker(b.keepaliveInterval)
	defer keepalive.Stop()
	var batch []cache.Event

	for {
		select {
		case e, ok := <-client.events:
			if !ok {
				if len(batch) > 0 {
					WriteBatch(w, flusher, batch)
				}
				return
			}
			// Skip events already sent during replay. Only clear the dedup
			// window once we see a definitive event beyond it (HistoryID > 0).
			if skipBelow > 0 && e.HistoryID > 0 {
				if e.HistoryID <= skipBelow {
					continue
				}
				skipBelow = 0
			}
			batch = append(batch, e)
		case <-batchTicker.C:
			if len(batch) > 0 {
				WriteBatch(w, flusher, batch)
				batch = batch[:0]
				keepalive.Reset(b.keepaliveInterval)
			}
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			if len(batch) > 0 {
				WriteBatch(w, flusher, batch)
			}
			return
		case <-client.done:
			if len(batch) > 0 {
				WriteBatch(w, flusher, batch)
			}
			return
		}
	}
}

func (b *Broadcaster) replayEvents(
	w io.Writer,
	flusher http.Flusher,
	afterID uint64,
	replayType cache.EventType,
) uint64 {
	writeSync := func() uint64 {
		count := b.replay.Count()
		WriteBatch(w, flusher, []cache.Event{{
			Type: cache.EventSync, Action: "full_sync", HistoryID: count,
		}})
		return count
	}

	// Detail/stack streams are ineligible for replay — send sync.
	if replayType == "" {
		return writeSync()
	}

	entries, ok := b.replay.Since(afterID)
	if !ok {
		return writeSync()
	}

	if len(entries) == 0 {
		return afterID
	}

	// Filter entries by type and convert to cache.Event (no Resource payload).
	var replay []cache.Event
	for _, e := range entries {
		if e.Type != replayType {
			continue
		}
		replay = append(replay, cache.Event{
			Type:      e.Type,
			Action:    e.Action,
			ID:        e.ResourceID,
			Name:      e.Name,
			HistoryID: e.ID,
		})
	}

	if len(replay) > 0 {
		WriteBatch(w, flusher, replay)
	}

	return entries[len(entries)-1].ID
}

// TypeMatcher returns a match function that accepts events of the given type.
// Sync events always pass through so clients can trigger a full refetch.
func TypeMatcher(typ cache.EventType) func(cache.Event) bool {
	return func(e cache.Event) bool {
		return e.Type == typ || e.Type == cache.EventSync
	}
}

// ResourceMatcher returns a match function for per-resource SSE streams.
// Sync events always pass through so clients can trigger a full refetch.
func ResourceMatcher(typ cache.EventType, id string) func(cache.Event) bool {
	switch typ {
	case cache.EventNode:
		return func(e cache.Event) bool {
			if e.Type == cache.EventSync {
				return true
			}
			if e.Type == cache.EventNode && e.ID == id {
				return true
			}
			if e.Type == cache.EventTask {
				if t, ok := e.Resource.(swarm.Task); ok {
					return t.NodeID == id
				}
			}
			return false
		}
	case cache.EventService:
		return func(e cache.Event) bool {
			if e.Type == cache.EventSync {
				return true
			}
			if e.Type == cache.EventService && e.ID == id {
				return true
			}
			if e.Type == cache.EventTask {
				if t, ok := e.Resource.(swarm.Task); ok {
					return t.ServiceID == id
				}
			}
			return false
		}
	case cache.EventTask:
		return func(e cache.Event) bool {
			return e.Type == cache.EventSync || (e.Type == cache.EventTask && e.ID == id)
		}
	default:
		// config, secret, network, volume — match by type+ID.
		// Cross-reference "ref_changed" events already have the correct type+ID.
		return func(e cache.Event) bool {
			return e.Type == cache.EventSync || (e.Type == typ && e.ID == id)
		}
	}
}

// StackMatcher returns a match function for stack SSE streams.
// Looks up the current stack membership from the cache on every event so that
// newly added or removed services are immediately reflected in the stream.
// Sync events always pass through so clients can trigger a full refetch.
func StackMatcher(c *cache.Cache, name string) func(cache.Event) bool {
	return func(e cache.Event) bool {
		if e.Type == cache.EventSync {
			return true
		}
		stack, ok := c.GetStack(name)
		if !ok {
			return false
		}
		switch e.Type {
		case cache.EventService:
			return slices.Contains(stack.Services, e.ID)
		case cache.EventConfig:
			return slices.Contains(stack.Configs, e.ID)
		case cache.EventSecret:
			return slices.Contains(stack.Secrets, e.ID)
		case cache.EventNetwork:
			return slices.Contains(stack.Networks, e.ID)
		case cache.EventVolume:
			return slices.Contains(stack.Volumes, e.ID)
		case cache.EventTask:
			if t, ok := e.Resource.(swarm.Task); ok {
				return slices.Contains(stack.Services, t.ServiceID)
			}
			return false
		default:
			return false
		}
	}
}

// Event is the JSON-LD enriched wire format for SSE event payloads.
type Event struct {
	AtID     string `json:"@id,omitempty"`
	AtType   string `json:"@type,omitempty"`
	Type     string `json:"type"`
	Action   string `json:"action"`
	ID       string `json:"id"`
	Resource any    `json:"resource,omitempty"`
}

func ToSSEEvent(e cache.Event) Event {
	return Event{
		AtID:     ResourcePath(e.Type, e.ID),
		AtType:   ResourceType(e.Type),
		Type:     string(e.Type),
		Action:   e.Action,
		ID:       e.ID,
		Resource: e.Resource,
	}
}

// ResourcePath returns the canonical API path for a resource.
func ResourcePath(typ cache.EventType, id string) string {
	switch typ {
	case cache.EventNode:
		return "/nodes/" + id
	case cache.EventService:
		return "/services/" + id
	case cache.EventTask:
		return "/tasks/" + id
	case cache.EventConfig:
		return "/configs/" + id
	case cache.EventSecret:
		return "/secrets/" + id
	case cache.EventNetwork:
		return "/networks/" + id
	case cache.EventVolume:
		return "/volumes/" + id
	case cache.EventStack:
		return "/stacks/" + id
	default:
		return ""
	}
}

// ResourceType returns the JSON-LD @type for a resource type string.
func ResourceType(typ cache.EventType) string {
	switch typ {
	case cache.EventNode:
		return "Node"
	case cache.EventService:
		return "Service"
	case cache.EventTask:
		return "Task"
	case cache.EventConfig:
		return "Config"
	case cache.EventSecret:
		return "Secret"
	case cache.EventNetwork:
		return "Network"
	case cache.EventVolume:
		return "Volume"
	case cache.EventStack:
		return "Stack"
	default:
		return ""
	}
}

func WriteBatch(w io.Writer, flusher http.Flusher, events []cache.Event) {
	var maxID uint64
	for _, e := range events {
		if e.HistoryID > maxID {
			maxID = e.HistoryID
		}
	}

	if len(events) == 1 {
		data, _ := json.Marshal(ToSSEEvent(events[0]))
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", maxID, events[0].Type, data)
	} else {
		enriched := make([]Event, len(events))
		for i, e := range events {
			enriched[i] = ToSSEEvent(e)
		}
		data, _ := json.Marshal(enriched)
		fmt.Fprintf(w, "id: %d\nevent: batch\ndata: %s\n\n", maxID, data)
	}
	flusher.Flush()
}
