package sse

import (
	"bytes"
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
	b := NewBroadcaster(0, noopErrorWriter, nil)
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
	b := NewBroadcaster(0, noopErrorWriter, nil)
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
	b := NewBroadcaster(0, noopErrorWriter, nil)
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
	b := NewBroadcaster(0, noopErrorWriter, nil)
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
	b := NewBroadcaster(0, noopErrorWriter, nil)
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
		typ  cache.EventType
		id   string
		want string
	}{
		{cache.EventNode, "n1", "/nodes/n1"},
		{cache.EventService, "s1", "/services/s1"},
		{cache.EventTask, "t1", "/tasks/t1"},
		{cache.EventConfig, "c1", "/configs/c1"},
		{cache.EventSecret, "sec1", "/secrets/sec1"},
		{cache.EventNetwork, "net1", "/networks/net1"},
		{cache.EventVolume, "vol1", "/volumes/vol1"},
		{cache.EventStack, "mystack", "/stacks/mystack"},
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
		typ  cache.EventType
		want string
	}{
		{cache.EventNode, "Node"},
		{cache.EventService, "Service"},
		{cache.EventTask, "Task"},
		{cache.EventConfig, "Config"},
		{cache.EventSecret, "Secret"},
		{cache.EventNetwork, "Network"},
		{cache.EventVolume, "Volume"},
		{cache.EventStack, "Stack"},
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
	b := NewBroadcaster(0, noopErrorWriter, nil)
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

func TestSSE_Keepalive(t *testing.T) {
	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, nil)
	b.keepaliveInterval = 50 * time.Millisecond
	defer b.Close()

	req := httptest.NewRequest("GET", "/events", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeSSE(w, req, nil, "")
		close(done)
	}()

	waitForClients(t, b, 1)
	waitForBody(t, w, ": keepalive")

	b.Close()
	<-done
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

func TestSSE_WriteBatch_UsesHistoryID(t *testing.T) {
	var buf bytes.Buffer
	f := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	events := []cache.Event{
		{Type: "service", Action: "update", ID: "s1", HistoryID: 42},
	}
	WriteBatch(&buf, f, events)

	output := buf.String()
	if !strings.Contains(output, "id: 42\n") {
		t.Errorf("expected id: 42, got %q", output)
	}
}

func TestSSE_WriteBatch_BatchUsesMaxHistoryID(t *testing.T) {
	var buf bytes.Buffer
	f := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	events := []cache.Event{
		{Type: "service", Action: "update", ID: "s1", HistoryID: 10},
		{Type: "service", Action: "update", ID: "s2", HistoryID: 12},
		{Type: "node", Action: "update", ID: "n1", HistoryID: 11},
	}
	WriteBatch(&buf, f, events)

	output := buf.String()
	if !strings.Contains(output, "id: 12\n") {
		t.Errorf("expected id: 12 (max), got %q", output)
	}
}

func TestSSE_WriteBatch_SyncUsesHistoryID(t *testing.T) {
	var buf bytes.Buffer
	f := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	events := []cache.Event{
		{Type: "sync", Action: "full_sync", HistoryID: 500},
	}
	WriteBatch(&buf, f, events)

	output := buf.String()
	if !strings.Contains(output, "id: 500\n") {
		t.Errorf("expected id: 500, got %q", output)
	}
}

// mockReplaySource implements ReplaySource for testing.
type mockReplaySource struct {
	entries  []cache.HistoryEntry
	count    uint64
	oldestID uint64 // simulate ring buffer oldest; 0 means no gap check
}

func (m *mockReplaySource) Since(afterID uint64) ([]cache.HistoryEntry, bool) {
	if afterID > m.count {
		return nil, false
	}
	if m.oldestID > 0 && afterID > 0 && afterID < m.oldestID {
		return nil, false
	}
	var result []cache.HistoryEntry
	for _, e := range m.entries {
		if e.ID > afterID {
			result = append(result, e)
		}
	}
	return result, true
}

func (m *mockReplaySource) Count() uint64 { return m.count }

func TestSSE_ReplayOnReconnect(t *testing.T) {
	replay := &mockReplaySource{
		entries: []cache.HistoryEntry{
			{ID: 5, Type: cache.EventService, Action: "update", ResourceID: "s1", Name: "web"},
			{ID: 6, Type: cache.EventNode, Action: "update", ResourceID: "n1", Name: "node-1"},
			{ID: 7, Type: cache.EventService, Action: "update", ResourceID: "s2", Name: "api"},
		},
		count: 7,
	}
	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, replay)
	defer b.Close()

	req := httptest.NewRequest("GET", "/services", nil)
	req.Header.Set("Last-Event-ID", "4")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeSSE(w, req, nil, cache.EventService)
		close(done)
	}()

	waitForClients(t, b, 1)

	// Wait for replay events to appear
	waitForBody(t, w, `"s2"`)

	b.Close()
	<-done

	body := w.bodyString()
	if !strings.Contains(body, `"s1"`) {
		t.Error("expected service s1 in replay")
	}
	if !strings.Contains(body, `"s2"`) {
		t.Error("expected service s2 in replay")
	}
	if strings.Contains(body, `"n1"`) {
		t.Error("node event should have been filtered out of replay")
	}
}

func TestSSE_ReplayTooOld_SendsSync(t *testing.T) {
	replay := &mockReplaySource{
		entries: []cache.HistoryEntry{
			{ID: 50, Type: cache.EventService, Action: "update", ResourceID: "s1", Name: "web"},
		},
		count:    100,
		oldestID: 50, // afterID=1 < 50, so Since returns ok=false
	}
	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, replay)
	defer b.Close()

	req := httptest.NewRequest("GET", "/services", nil)
	req.Header.Set("Last-Event-ID", "1")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeSSE(w, req, nil, cache.EventService)
		close(done)
	}()

	waitForClients(t, b, 1)
	waitForBody(t, w, "event: sync")

	b.Close()
	<-done

	body := w.bodyString()
	if !strings.Contains(body, `"action":"full_sync"`) {
		t.Errorf("expected sync event, got: %s", body)
	}
	if !strings.Contains(body, "id: 100\n") {
		t.Errorf("expected sync id: 100, got: %s", body)
	}
}

func TestSSE_ReplayIneligible_SendsSync(t *testing.T) {
	replay := &mockReplaySource{
		entries: []cache.HistoryEntry{
			{ID: 5, Type: cache.EventService, Action: "update", ResourceID: "s1", Name: "web"},
		},
		count: 10,
	}
	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, replay)
	defer b.Close()

	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Last-Event-ID", "4")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		// Empty replayType means ineligible for replay
		b.ServeSSE(w, req, nil, "")
		close(done)
	}()

	waitForClients(t, b, 1)
	waitForBody(t, w, "event: sync")

	b.Close()
	<-done

	body := w.bodyString()
	if !strings.Contains(body, `"action":"full_sync"`) {
		t.Errorf("expected sync event, got: %s", body)
	}
}

func TestSSE_NoLastEventID_NoReplay(t *testing.T) {
	replay := &mockReplaySource{
		entries: []cache.HistoryEntry{
			{ID: 5, Type: cache.EventService, Action: "update", ResourceID: "s1", Name: "web"},
		},
		count: 10,
	}
	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, replay)
	defer b.Close()

	req := httptest.NewRequest("GET", "/services", nil)
	// No Last-Event-ID header
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeSSE(w, req, nil, cache.EventService)
		close(done)
	}()

	waitForClients(t, b, 1)

	// Send a real event to confirm we're connected and receiving
	b.Broadcast(cache.Event{Type: cache.EventService, Action: "update", ID: "s99", HistoryID: 20})
	waitForBody(t, w, `"s99"`)

	b.Close()
	<-done

	body := w.bodyString()
	if strings.Contains(body, "event: sync") {
		t.Error("should not have sent sync event without Last-Event-ID")
	}
	if strings.Contains(body, `"s1"`) {
		t.Error("should not have replayed events without Last-Event-ID")
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
