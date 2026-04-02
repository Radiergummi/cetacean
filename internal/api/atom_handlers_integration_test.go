package api

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	atomxml "github.com/radiergummi/cetacean/internal/api/atom"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestAtomListHandler_ReturnsValidAtom(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api"}},
	})

	h := newTestHandlers(t, withCache(c))
	handler := h.atomListHandler("Services", cache.EventService)

	req := httptest.NewRequest("GET", "/services", nil)
	req = withContentType(req, ContentTypeAtom)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/atom+xml;charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/atom+xml;charset=utf-8", ct)
	}

	if w.Header().Get("ETag") == "" {
		t.Error("expected ETag header")
	}

	var feed atomxml.Feed
	if err := xml.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}

	if feed.Title != "Services" {
		t.Errorf("feed.Title = %q, want %q", feed.Title, "Services")
	}

	if feed.Author == nil || feed.Author.Name != "Cetacean" {
		t.Errorf("feed.Author = %v, want &{Cetacean}", feed.Author)
	}

	if len(feed.Entries) != 2 {
		t.Errorf("len(feed.Entries) = %d, want 2", len(feed.Entries))
	}
}

func TestAtomDetailHandler_ReturnsValidAtom(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})

	h := newTestHandlers(t, withCache(c))
	handler := h.atomDetailHandler(cache.EventNode, "id", func(id string) string {
		if n, ok := h.cache.GetNode(id); ok {
			return n.Description.Hostname
		}
		return id
	})

	req := httptest.NewRequest("GET", "/nodes/node1", nil)
	req.SetPathValue("id", "node1")
	req = withContentType(req, ContentTypeAtom)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var feed atomxml.Feed
	if err := xml.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}

	if feed.Title != "worker-1" {
		t.Errorf("feed.Title = %q, want %q", feed.Title, "worker-1")
	}
}

func TestAtomNilHandler_Returns406(t *testing.T) {
	h := newTestHandlers(t)
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// contentNegotiated with nil atom handler should return 406
	handler := contentNegotiated(h.HandleCluster, nil, spa)

	req := httptest.NewRequest("GET", "/cluster", nil)
	req = req.WithContext(context.WithValue(req.Context(), contentTypeKey{}, ContentTypeAtom))
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusNotAcceptable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotAcceptable)
	}
}

func TestHandleAtomHistory_ReturnsGlobalFeed(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})

	h := newTestHandlers(t, withCache(c))

	req := httptest.NewRequest("GET", "/history", nil)
	req = withContentType(req, ContentTypeAtom)
	w := httptest.NewRecorder()

	h.HandleAtomHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var feed atomxml.Feed
	if err := xml.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}

	if feed.Title != "History" {
		t.Errorf("feed.Title = %q, want %q", feed.Title, "History")
	}

	// Should contain entries for both service and node
	if len(feed.Entries) != 2 {
		t.Errorf("len(feed.Entries) = %d, want 2", len(feed.Entries))
	}
}

func TestAtomListHandler_ConditionalNotModified(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	h := newTestHandlers(t, withCache(c))
	handler := h.atomListHandler("Services", cache.EventService)

	// First request to get the ETag
	req := httptest.NewRequest("GET", "/services", nil)
	req = withContentType(req, ContentTypeAtom)
	w := httptest.NewRecorder()
	handler(w, req)

	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header on first request")
	}

	// Second request with If-None-Match
	req2 := httptest.NewRequest("GET", "/services", nil)
	req2 = withContentType(req2, ContentTypeAtom)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	handler(w2, req2)

	if w2.Code != http.StatusNotModified {
		t.Errorf("status = %d, want %d", w2.Code, http.StatusNotModified)
	}
}

func TestHandleAtomSearch_FiltersEntriesByName(t *testing.T) {
	c := cache.New(nil)

	// Seed entries: two services with different names, plus a node.
	c.SetService(swarm.Service{
		ID:   "svc-match",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "myservice"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc-other",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "database"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})

	h := newTestHandlers(t, withCache(c))

	t.Run("returns matching entries", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/search?q=myservice", nil)
		req = withContentType(req, ContentTypeAtom)
		w := httptest.NewRecorder()

		h.HandleAtomSearch(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: body = %s", w.Code, http.StatusOK, w.Body.String())
		}

		var feed atomxml.Feed
		if err := xml.Unmarshal(w.Body.Bytes(), &feed); err != nil {
			t.Fatalf("invalid XML: %v", err)
		}

		if feed.Title != "Cetacean — Search: myservice" {
			t.Errorf("feed.Title = %q, want %q", feed.Title, "Cetacean — Search: myservice")
		}

		if len(feed.Entries) != 1 {
			t.Errorf("len(feed.Entries) = %d, want 1", len(feed.Entries))
		}
	})

	t.Run("returns 400 when query is missing", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/search", nil)
		req = withContentType(req, ContentTypeAtom)
		w := httptest.NewRecorder()

		h.HandleAtomSearch(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("returns 400 when query is whitespace only", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/search?q=+++", nil)
		req = withContentType(req, ContentTypeAtom)
		w := httptest.NewRecorder()

		h.HandleAtomSearch(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("returns 400 when query exceeds 200 characters", func(t *testing.T) {
		long := strings.Repeat("x", 201)
		req := httptest.NewRequest("GET", "/search?q="+long, nil)
		req = withContentType(req, ContentTypeAtom)
		w := httptest.NewRecorder()

		h.HandleAtomSearch(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})
}
