package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

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
	mu      sync.RWMutex
	clients map[*sseClient]struct{}
	closed  bool
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		clients: make(map[*sseClient]struct{}),
	}
}

func (b *Broadcaster) Broadcast(e cache.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
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
}

func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for c := range b.clients {
		close(c.done)
	}
}

func (b *Broadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
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
		http.Error(w, "too many SSE connections", http.StatusServiceUnavailable)
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

	for {
		select {
		case <-r.Context().Done():
			return
		case <-client.done:
			return
		case e := <-client.events:
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
			flusher.Flush()
		}
	}
}
