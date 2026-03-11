package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"

	"cetacean/internal/cache"
)

type sseClient struct {
	events chan cache.Event
	types  map[string]bool // nil means all types
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
				if c.types != nil && !c.types[e.Type] {
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
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "streaming not supported")
		return
	}

	var types map[string]bool
	if t := r.URL.Query().Get("types"); t != "" {
		types = make(map[string]bool)
		for _, typ := range strings.Split(t, ",") {
			types[strings.TrimSpace(typ)] = true
		}
	}

	client := &sseClient{
		events: make(chan cache.Event, 64),
		types:  types,
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
	b.clients[client] = struct{}{}
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, client)
		b.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

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

func writeBatch(w io.Writer, flusher http.Flusher, events []cache.Event, eventID *uint64) {
	*eventID++
	if len(events) == 1 {
		data, _ := json.Marshal(events[0])
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", *eventID, events[0].Type, data)
	} else {
		data, _ := json.Marshal(events)
		fmt.Fprintf(w, "id: %d\nevent: batch\ndata: %s\n\n", *eventID, data)
	}
	flusher.Flush()
}
