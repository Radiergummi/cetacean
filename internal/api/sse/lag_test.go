package sse

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

// gatedRecorder holds its first write until released, standing in for a
// client that stops reading for a while.
type gatedRecorder struct {
	flushRecorder
	entered  chan struct{}
	released chan struct{}
	once     sync.Once
}

func (g *gatedRecorder) Write(b []byte) (int, error) {
	g.once.Do(func() {
		close(g.entered)
		<-g.released
	})

	return g.flushRecorder.Write(b)
}

// A client too slow to take every event must not be handed an id past the
// events it missed: a reconnect from that id would skip them for good.
func TestSSE_ASlowClientIsResyncedRatherThanSkippedAhead(t *testing.T) {
	h := cache.NewHistory(1000)
	b := NewBroadcaster(5*time.Millisecond, noopErrorWriter, h)
	defer b.Close()

	w := &gatedRecorder{
		flushRecorder: flushRecorder{ResponseRecorder: httptest.NewRecorder()},
		entered:       make(chan struct{}),
		released:      make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		b.ServeSSE(w, httptest.NewRequest("GET", "/events", nil), nil, ReplayAll)
		close(done)
	}()
	waitForClients(t, b, 1)

	broadcast := func() {
		id := h.Append(cache.HistoryEntry{Type: cache.EventService, Action: "update"})
		b.Broadcast(cache.Event{
			Type: cache.EventService, Action: "update", ID: fmt.Sprint(id), HistoryID: id,
		})
	}

	broadcast()
	<-w.entered

	// Far more than the client's buffer holds, paced so the inbox never fills.
	for range 200 {
		broadcast()
		time.Sleep(50 * time.Microsecond)
	}
	close(w.released)

	waitForBody(t, &w.flushRecorder, "event: sync")
	b.Close()
	<-done

	frames := strings.Split(strings.TrimSpace(w.bodyString()), "\n\n")
	if len(frames) < 2 {
		t.Fatalf("frames = %q, want the first event and then a sync", frames)
	}
	if !strings.Contains(frames[1], "event: sync") {
		t.Errorf("the frame after the gap is %q, want a sync", frames[1])
	}
	if want := "id: " + b.EventID(h.Count()); !strings.Contains(frames[1], want) {
		t.Errorf("the sync is %q, want it at the current position %q", frames[1], want)
	}
}

// An event the broadcaster itself had no room for missed every client it was
// meant for, so each of those is resynced too.
func TestSSE_AFullInboxLagsTheClientsItWasFor(t *testing.T) {
	services := &sseClient{match: TypeMatcher(cache.EventService)}
	nodes := &sseClient{match: TypeMatcher(cache.EventNode)}
	b := &Broadcaster{
		clients: map[*sseClient]struct{}{services: {}, nodes: {}},
		inbox:   make(chan cache.Event), // nothing reads it: always full
	}

	b.Broadcast(cache.Event{Type: cache.EventService, ID: "s1", HistoryID: 1})

	if !services.lagged.Load() {
		t.Error("a client matching the dropped event was not marked lagged")
	}
	if nodes.lagged.Load() {
		t.Error("a client not matching the dropped event was marked lagged")
	}
}

// A client that stops reading must not hold its handler, its slot and
// shutdown for as long as the connection stays open.
func TestSSE_AStalledClientIsDisconnected(t *testing.T) {
	original := writeTimeout
	writeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { writeTimeout = original })

	b := NewBroadcaster(5*time.Millisecond, noopErrorWriter, nil)
	defer b.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.ServeSSE(w, r, nil, ReplayAll)
	}))
	defer server.Close()

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // test cleanup

	if _, err := fmt.Fprintf(conn, "GET /events HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := http.ReadResponse(bufio.NewReader(conn), nil); err != nil {
		t.Fatal(err)
	}
	waitForClients(t, b, 1)

	// Large enough to fill both socket buffers, after which nothing is read.
	payload := strings.Repeat("x", 1<<20)
	for i := range 64 {
		b.Broadcast(cache.Event{
			Type: cache.EventService, ID: fmt.Sprint(i), HistoryID: uint64(i + 1),
			Resource: payload,
		})
	}

	deadline := time.After(10 * time.Second)
	for {
		b.mu.RLock()
		n := len(b.clients)
		b.mu.RUnlock()
		if n == 0 {
			return
		}

		select {
		case <-deadline:
			t.Fatal("the stream to a client that stopped reading never ended")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
