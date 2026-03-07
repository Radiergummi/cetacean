package api

import (
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

// flushRecorder implements http.Flusher for testing SSE.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

// Ensure flushRecorder implements http.Flusher.
var _ http.Flusher = (*flushRecorder)(nil)
