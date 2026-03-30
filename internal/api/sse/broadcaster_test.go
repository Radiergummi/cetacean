package sse

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func noopErrorWriter(w http.ResponseWriter, _ *http.Request, _, detail string) {
	http.Error(w, detail, http.StatusInternalServerError)
}

// waitForClients polls until the broadcaster has at least n clients registered.
func waitForClients(t *testing.T, b *Broadcaster, n int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		b.mu.RLock()
		count := len(b.clients)
		b.mu.RUnlock()
		if count >= n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d SSE client(s), have %d", n, count)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// waitForBody polls until the recorder's body contains the expected substring.
func waitForBody(t *testing.T, w *flushRecorder, substr string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if strings.Contains(w.bodyString(), substr) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %q in body, got: %s", substr, w.bodyString())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestSSE_BroadcastsEvents(t *testing.T) {
	b := NewBroadcaster(0, noopErrorWriter)
	defer b.Close()

	req := httptest.NewRequest("GET", "/events", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(w, req)
		close(done)
	}()

	waitForClients(t, b, 1)

	b.Broadcast(cache.Event{Type: "node", Action: "update", ID: "n1"})

	waitForBody(t, w, "event: node")
	b.Close()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, `"action":"update"`) {
		t.Errorf("expected action:update in body, got: %s", body)
	}
}

func TestSSE_FiltersByType(t *testing.T) {
	b := NewBroadcaster(0, noopErrorWriter)
	defer b.Close()

	req := httptest.NewRequest("GET", "/events?types=service", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(w, req)
		close(done)
	}()

	waitForClients(t, b, 1)

	b.Broadcast(cache.Event{Type: "node", Action: "update", ID: "n1"})
	b.Broadcast(cache.Event{Type: "service", Action: "update", ID: "s1"})

	waitForBody(t, w, "event: service")
	b.Close()
	<-done

	body := w.Body.String()
	if strings.Contains(body, "event: node") {
		t.Error("node event should have been filtered out")
	}
}

func TestSSE_BatchesRapidEvents(t *testing.T) {
	b := NewBroadcaster(0, noopErrorWriter)
	defer b.Close()

	req := httptest.NewRequest("GET", "/events", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(w, req)
		close(done)
	}()

	waitForClients(t, b, 1)

	// Send 5 events as fast as possible so they land in the same batch window
	for i := range 5 {
		b.Broadcast(cache.Event{Type: "task", Action: "update", ID: fmt.Sprintf("t%d", i)})
	}

	// Wait for all events to appear (either individual or batched)
	waitForBody(t, w, `"t4"`)
	b.Close()
	<-done

	body := w.Body.String()

	// We should see at least one event line (either individual "event: task" or "event: batch")
	hasIndividual := strings.Contains(body, "event: task")
	hasBatch := strings.Contains(body, "event: batch")
	if !hasIndividual && !hasBatch {
		t.Fatalf("expected event: task or event: batch in body, got: %s", body)
	}

	// Every event/batch must have an id: field
	if !strings.Contains(body, "id: ") {
		t.Errorf("expected id: field in body, got: %s", body)
	}

	// If we got a batch event, verify it contains a JSON array
	if hasBatch {
		for line := range strings.SplitSeq(body, "\n") {
			if strings.HasPrefix(line, "data: [") {
				// Verify it's a valid JSON array containing our events
				if !strings.Contains(line, `"t0"`) || !strings.Contains(line, `"t4"`) {
					t.Errorf("batch data should contain all events, got: %s", line)
				}
			}
		}
	}
}

func TestSSE_EventContainsJSONLD(t *testing.T) {
	b := NewBroadcaster(0, noopErrorWriter)
	defer b.Close()

	req := httptest.NewRequest("GET", "/events", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(w, req)
		close(done)
	}()

	waitForClients(t, b, 1)

	b.Broadcast(cache.Event{Type: "service", Action: "update", ID: "abc123"})

	waitForBody(t, w, `"@id":"/services/abc123"`)
	b.Close()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, `"@type":"Service"`) {
		t.Errorf("expected @type field in body, got: %s", body)
	}
}

func TestSSE_BatchEventContainsJSONLD(t *testing.T) {
	b := NewBroadcaster(0, noopErrorWriter)
	defer b.Close()

	req := httptest.NewRequest("GET", "/events", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(w, req)
		close(done)
	}()

	waitForClients(t, b, 1)

	// Send multiple events fast to trigger batching
	for i := range 3 {
		b.Broadcast(cache.Event{Type: "task", Action: "update", ID: fmt.Sprintf("t%d", i)})
	}

	waitForBody(t, w, `"@id":"/tasks/t0"`)
	b.Close()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, `"@type":"Task"`) {
		t.Errorf("expected @type Task in body, got: %s", body)
	}
}

func TestResourcePath(t *testing.T) {
	tests := []struct {
		typ, id, want string
	}{
		{"node", "n1", "/nodes/n1"},
		{"service", "s1", "/services/s1"},
		{"task", "t1", "/tasks/t1"},
		{"config", "c1", "/configs/c1"},
		{"secret", "sec1", "/secrets/sec1"},
		{"network", "net1", "/networks/net1"},
		{"volume", "vol1", "/volumes/vol1"},
		{"stack", "mystack", "/stacks/mystack"},
		{"unknown", "x", ""},
	}
	for _, tt := range tests {
		got := ResourcePath(tt.typ, tt.id)
		if got != tt.want {
			t.Errorf("ResourcePath(%q, %q) = %q, want %q", tt.typ, tt.id, got, tt.want)
		}
	}
}

func TestResourceType(t *testing.T) {
	tests := []struct {
		typ, want string
	}{
		{"node", "Node"},
		{"service", "Service"},
		{"task", "Task"},
		{"config", "Config"},
		{"secret", "Secret"},
		{"network", "Network"},
		{"volume", "Volume"},
		{"stack", "Stack"},
		{"unknown", ""},
	}
	for _, tt := range tests {
		got := ResourceType(tt.typ)
		if got != tt.want {
			t.Errorf("ResourceType(%q) = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

func TestSSE_429OnConnectionLimit(t *testing.T) {
	b := NewBroadcaster(0, noopErrorWriter)
	defer b.Close()

	req := httptest.NewRequest("GET", "/events", nil)
	w := httptest.NewRecorder()

	// Artificially fill up clients to max
	b.mu.Lock()
	for range MaxClients {
		c := &sseClient{
			events: make(chan cache.Event, 1),
			done:   make(chan struct{}),
		}
		b.clients[c] = struct{}{}
	}
	b.mu.Unlock()

	// This recorder doesn't implement Flusher, so we need a flushRecorder
	fw := &flushRecorder{ResponseRecorder: w}
	b.ServeHTTP(fw, req)

	// noopErrorWriter writes 500; the real error writer writes 429.
	// We just verify the error writer was called (body contains the detail).
	if !strings.Contains(w.Body.String(), "too many SSE connections") {
		t.Errorf("expected error about too many connections, got: %s", w.Body.String())
	}
}

func TestSSE_ResourceMatcher_Config(t *testing.T) {
	match := ResourceMatcher("config", "cfg1")
	if !match(cache.Event{Type: "config", Action: "update", ID: "cfg1"}) {
		t.Error("should match direct config event")
	}
	if !match(cache.Event{Type: "config", Action: "ref_changed", ID: "cfg1"}) {
		t.Error("should match ref_changed event")
	}
	if match(cache.Event{Type: "config", Action: "update", ID: "cfg2"}) {
		t.Error("should not match different config")
	}
	if match(cache.Event{Type: "service", Action: "update", ID: "svc1"}) {
		t.Error("should not match service event")
	}
}

func TestSSE_ResourceMatcher_Service(t *testing.T) {
	match := ResourceMatcher("service", "svc1")
	if !match(cache.Event{Type: "service", Action: "update", ID: "svc1"}) {
		t.Error("should match direct service event")
	}
	if !match(
		cache.Event{
			Type:     "task",
			Action:   "update",
			ID:       "t1",
			Resource: swarm.Task{ServiceID: "svc1"},
		},
	) {
		t.Error("should match task for this service")
	}
	if match(
		cache.Event{
			Type:     "task",
			Action:   "update",
			ID:       "t2",
			Resource: swarm.Task{ServiceID: "svc2"},
		},
	) {
		t.Error("should not match task for different service")
	}
}

func TestSSE_ResourceMatcher_Node(t *testing.T) {
	match := ResourceMatcher("node", "n1")
	if !match(cache.Event{Type: "node", Action: "update", ID: "n1"}) {
		t.Error("should match direct node event")
	}
	if !match(
		cache.Event{Type: "task", Action: "update", ID: "t1", Resource: swarm.Task{NodeID: "n1"}},
	) {
		t.Error("should match task on this node")
	}
	if match(
		cache.Event{Type: "task", Action: "update", ID: "t2", Resource: swarm.Task{NodeID: "n2"}},
	) {
		t.Error("should not match task on different node")
	}
}

func TestSSE_TypeMatcher(t *testing.T) {
	match := TypeMatcher("node")
	if !match(cache.Event{Type: "node", Action: "update", ID: "n1"}) {
		t.Error("should match node event")
	}
	if match(cache.Event{Type: "service", Action: "update", ID: "s1"}) {
		t.Error("should not match service event")
	}
}

// flushRecorder implements http.Flusher for testing SSE with thread-safe body access.
type flushRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (f *flushRecorder) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ResponseRecorder.Write(b)
}

func (f *flushRecorder) bodyString() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Body.String()
}

func (f *flushRecorder) Flush() {}

// Ensure flushRecorder implements http.Flusher.
var _ http.Flusher = (*flushRecorder)(nil)
