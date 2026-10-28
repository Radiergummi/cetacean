package sse

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Every client is handed the same encoded payload, not one encoding each.
func TestFanOutEncodesEachEventOnce(t *testing.T) {
	b := NewBroadcaster(time.Hour, nil)
	defer b.Close()
	b.SetBasePath("/cetacean")

	first := &sseClient{events: make(chan frameEvent, 1), done: make(chan struct{})}
	second := &sseClient{events: make(chan frameEvent, 1), done: make(chan struct{})}
	b.mu.Lock()
	b.clients[first] = struct{}{}
	b.clients[second] = struct{}{}
	b.mu.Unlock()

	b.Broadcast(cache.Event{
		Type: cache.EventService, Action: "update", ID: "s1",
		Resource: swarm.Service{ID: "s1"}, HistoryID: 1,
	})

	a, c := <-first.events, <-second.events
	if len(a.data) == 0 || &a.data[0] != &c.data[0] {
		t.Fatal("the clients were handed separate encodings of one event")
	}

	want, _ := json.Marshal(ToSSEEvent(a.Event, "/cetacean"))
	if !bytes.Equal(a.data, want) {
		t.Errorf("payload = %s, want %s", a.data, want)
	}
}

// A batch joined from pre-encoded payloads is the array a single encode makes.
func TestBatchOfEncodedEventsMatchesEncodingTheArray(t *testing.T) {
	events := []cache.Event{
		{
			Type: cache.EventService, Action: "update", ID: "s1",
			Resource: swarm.Service{ID: "s1"}, HistoryID: 1,
		},
		{Type: cache.EventNode, Action: "remove", ID: "n1", HistoryID: 2},
		{Type: cache.EventSync, Action: "full_sync", HistoryID: 2},
	}

	frame := make([]frameEvent, len(events))
	enriched := make([]Event, len(events))
	for i, e := range events {
		frame[i] = frameEvent{Event: e, data: encodeEvent(e, "/p")}
		enriched[i] = ToSSEEvent(e, "/p")
	}

	var got bytes.Buffer
	f := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	if err := writeBatch(&got, f, frame, "/p", "e"); err != nil {
		t.Fatal(err)
	}

	data, _ := json.Marshal(enriched)
	want := fmt.Sprintf("id: e-2\nevent: batch\ndata: %s\n\n", data)
	if got.String() != want {
		t.Errorf("frame =\n%q\nwant\n%q", got.String(), want)
	}
}
