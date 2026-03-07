package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cetacean/internal/cache"
)

func TestSSE_BroadcastsEvents(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	req := httptest.NewRequest("GET", "/api/events", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	b.Broadcast(cache.Event{Type: "node", Action: "update", ID: "n1"})

	time.Sleep(50 * time.Millisecond)
	b.Close()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, "event: node") {
		t.Errorf("expected event: node in body, got: %s", body)
	}
	if !strings.Contains(body, `"action":"update"`) {
		t.Errorf("expected action:update in body, got: %s", body)
	}
}

func TestSSE_FiltersByType(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	req := httptest.NewRequest("GET", "/api/events?types=service", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	b.Broadcast(cache.Event{Type: "node", Action: "update", ID: "n1"})
	b.Broadcast(cache.Event{Type: "service", Action: "update", ID: "s1"})

	time.Sleep(50 * time.Millisecond)
	b.Close()
	<-done

	body := w.Body.String()
	if strings.Contains(body, "event: node") {
		t.Error("node event should have been filtered out")
	}
	if !strings.Contains(body, "event: service") {
		t.Error("service event should have been included")
	}
}

func TestSSE_BatchesRapidEvents(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	req := httptest.NewRequest("GET", "/api/events", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	// Send 5 events as fast as possible so they land in the same batch window
	for i := 0; i < 5; i++ {
		b.Broadcast(cache.Event{Type: "task", Action: "update", ID: fmt.Sprintf("t%d", i)})
	}

	// Wait for the batch ticker to fire and then close
	time.Sleep(200 * time.Millisecond)
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
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "data: [") {
				// Verify it's a valid JSON array containing our events
				if !strings.Contains(line, `"t0"`) || !strings.Contains(line, `"t4"`) {
					t.Errorf("batch data should contain all events, got: %s", line)
				}
			}
		}
	}
}

// flushRecorder implements http.Flusher for testing SSE.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

// Ensure flushRecorder implements http.Flusher.
var _ http.Flusher = (*flushRecorder)(nil)
