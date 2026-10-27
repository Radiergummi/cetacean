package sse

import (
	"context"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

// reconnect opens /events with lastID, waits for a live marker event to come
// through after whatever replay the cursor earned, and returns the body.
func reconnect(t *testing.T, b *Broadcaster, lastID string) string {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequestWithContext(ctx, "GET", "/events", nil)
	req.Header.Set("Last-Event-ID", lastID)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		b.ServeSSE(w, req, nil, ReplayAll)
	}()

	waitForClients(t, b, 1)
	b.Broadcast(cache.Event{Type: cache.EventNode, Action: "update", ID: "marker"})
	waitForBody(t, w, `"marker"`)
	cancel()
	<-done

	return w.bodyString()
}

func appendEntries(h *cache.History, n int) {
	for range n {
		h.Append(cache.HistoryEntry{Type: cache.EventService, Action: "update", ResourceID: "s"})
	}
}

// A resync replaces the cache without a history entry, so a cursor from
// before it cannot be caught up by replay: the client has to refetch.
func TestSSE_ReplaySendsSyncForAResyncSinceTheCursor(t *testing.T) {
	h := cache.NewHistory(100)
	appendEntries(h, 3)

	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, h)
	defer b.Close()

	b.Broadcast(syncEvent(h.Count()))
	appendEntries(h, 1)

	for _, test := range []struct {
		cursor string
		sync   bool
	}{
		{b.EventID(2), true},  // before the resync
		{b.EventID(3), true},  // the entry the resync followed, but not the resync
		{b.EventID(4), false}, // an entry after the resync
	} {
		if got := strings.Contains(reconnect(t, b, test.cursor), "event: sync"); got != test.sync {
			t.Errorf("cursor %s: sync sent = %v, want %v", test.cursor, got, test.sync)
		}
	}
}

var syncID = regexp.MustCompile(`id: (\S+)\nevent: sync\n`)

// A client that received the resync's own frame is caught up, until the next.
func TestSSE_ReplayKnowsAClientSawTheResync(t *testing.T) {
	h := cache.NewHistory(100)
	appendEntries(h, 3)

	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, h)
	defer b.Close()

	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequestWithContext(ctx, "GET", "/events", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.ServeSSE(w, req, nil, ReplayAll)
	}()

	waitForClients(t, b, 1)
	b.Broadcast(syncEvent(h.Count()))
	waitForBody(t, w, "event: sync")
	cancel()
	<-done

	match := syncID.FindStringSubmatch(w.bodyString())
	if match == nil {
		t.Fatalf("no sync id in %q", w.bodyString())
	}
	cursor := match[1]

	if strings.Contains(reconnect(t, b, cursor), "event: sync") {
		t.Error("a client that saw the resync was sent another")
	}

	b.Broadcast(syncEvent(h.Count()))
	if !strings.Contains(reconnect(t, b, cursor), "event: sync") {
		t.Error("a client that missed the second resync at the same position was not sent it")
	}
}
