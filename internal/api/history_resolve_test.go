package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func historyItems(t *testing.T, w *httptest.ResponseRecorder) []cache.HistoryEntry {
	t.Helper()

	var resp CollectionResponse[cache.HistoryEntry]
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	return resp.Items
}

// A name reaches a detail page through the canonical redirect, but the
// timeline takes its identifier in a query parameter no redirect rewrites, so
// a name used to read as an empty feed.
func TestHandleHistoryResolvesANameToTheIDTheRingKeysBy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "shop_web"}},
	})

	h := newTestHandlers(t, withCache(c))

	for _, identifier := range []string{"svc1", "shop_web"} {
		req := httptest.NewRequest("GET", "/history?type=service&resourceId="+identifier, nil)
		w := httptest.NewRecorder()
		h.HandleHistory(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("resourceId=%s: status=%d, want 200", identifier, w.Code)
		}

		if items := historyItems(t, w); len(items) != 1 {
			t.Errorf("resourceId=%s: got %d entries, want 1", identifier, len(items))
		}
	}
}

// Without a type the search spans every type, so the one name the dashboard
// cannot label still resolves as long as nothing else claims it.
func TestHandleHistoryResolvesANameWithoutAType(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-01"}})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/history?resourceId=worker-01", nil)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	if items := historyItems(t, w); len(items) != 1 {
		t.Errorf("got %d entries, want 1", len(items))
	}
}

// The ring outlives the cache's knowledge of a resource, so an identifier that
// resolves to nothing has to keep reaching the entries stored under it.
func TestHandleHistoryKeepsAnIdentifierTheCacheHasForgotten(t *testing.T) {
	c := cache.New(nil)
	c.History().Append(cache.HistoryEntry{
		Timestamp:  time.Now(),
		Type:       cache.EventService,
		Action:     "remove",
		ResourceID: "svc-gone",
		Name:       "retired",
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/history?resourceId=svc-gone", nil)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	if items := historyItems(t, w); len(items) != 1 {
		t.Errorf("got %d entries, want 1", len(items))
	}
}
