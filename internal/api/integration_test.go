package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	json "github.com/goccy/go-json"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

func setupIntegrationRouter(t *testing.T) http.Handler {
	t.Helper()
	c := cache.New(nil)
	// Populate with enough data for pagination testing (default limit=50).
	for i := range 60 {
		c.SetNode(swarm.Node{
			ID:          fmt.Sprintf("node-%d", i),
			Description: swarm.NodeDescription{Hostname: fmt.Sprintf("worker-%d", i)},
		})
	}
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	b := sse.NewBroadcaster(100*time.Millisecond, noopErrorWriter)
	h := newTestHandlers(t, withCache(c), withBroadcaster(b))
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>SPA</html>")) //nolint:errcheck
	})

	specBytes, _ := os.ReadFile("../../api/openapi.yaml")
	return NewRouter(
		h,
		b,
		nil,
		spa,
		specBytes,
		[]byte("/* scalar */"),
		false,
		true,
		&auth.NoneProvider{},
		"",
		nil,
	)
}

func TestContentNegotiationIntegration(t *testing.T) {
	router := setupIntegrationRouter(t)

	t.Run("JSON request returns JSON-LD collection", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nodes", nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type=%q, want application/json", ct)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}
		if body["@context"] == nil {
			t.Error("response missing @context")
		}
		if body["@type"] == nil {
			t.Error("response missing @type")
		}
		if body["@type"] != "Collection" {
			t.Errorf("@type=%v, want Collection", body["@type"])
		}
	})

	t.Run("HTML request returns SPA", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nodes", nil)
		req.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type=%q, want text/html", ct)
		}
		if !strings.Contains(w.Body.String(), "SPA") {
			t.Error("response body does not contain SPA content")
		}
	})

	t.Run("extension override wins over Accept header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nodes.json", nil)
		req.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type=%q, want application/json (extension should win)", ct)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}
		if body["@context"] == nil {
			t.Error("response missing @context")
		}
	})

	t.Run("SSE on non-SSE endpoint returns 406", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/history", nil)
		req.Header.Set("Accept", "text/event-stream")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotAcceptable {
			t.Fatalf("status=%d, want 406", w.Code)
		}
	})

	t.Run("406 error is RFC 9457 problem+json", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/history", nil)
		req.Header.Set("Accept", "text/event-stream")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "application/problem+json") {
			t.Fatalf("Content-Type=%q, want application/problem+json", ct)
		}
		var p ProblemDetail
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatalf("failed to decode problem: %v", err)
		}
		if p.Status != http.StatusNotAcceptable {
			t.Errorf("problem status=%d, want 406", p.Status)
		}
		if p.Type != "/api/errors/API001" {
			t.Errorf("problem type=%q, want /api/errors/API001", p.Type)
		}
	})

	t.Run("detail endpoint has @id and @type and node wrapper", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nodes/node-0", nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", w.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}
		if body["@id"] == nil {
			t.Error("response missing @id")
		}
		if body["@type"] == nil {
			t.Error("response missing @type")
		}
		if body["@type"] != "Node" {
			t.Errorf("@type=%v, want Node", body["@type"])
		}
		if body["node"] == nil {
			t.Error("response missing node wrapper key")
		}
	})

	t.Run("404 for nonexistent node is RFC 9457", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nodes/nonexistent", nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "application/problem+json") {
			t.Fatalf("Content-Type=%q, want application/problem+json", ct)
		}
		var p ProblemDetail
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatalf("failed to decode problem: %v", err)
		}
		if p.Status != http.StatusNotFound {
			t.Errorf("problem status=%d, want 404", p.Status)
		}
	})

	t.Run("meta endpoint works without content negotiation", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/-/health", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", w.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}
		if body["status"] != "ok" {
			t.Errorf("health status=%q, want ok", body["status"])
		}
	})

	t.Run("pagination Link headers present when multiple pages", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nodes", nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", w.Code)
		}
		allLinks := strings.Join(w.Header().Values("Link"), ", ")
		if !strings.Contains(allLinks, `rel="next"`) {
			t.Errorf("expected Link header with rel=next for paginated results, got %q", allLinks)
		}

		// Verify the collection metadata indicates more items
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}
		total, ok := body["total"].(float64)
		if !ok || total != 60 {
			t.Errorf("total=%v, want 60", body["total"])
		}
	})

	t.Run("ETag and conditional request returns 304", func(t *testing.T) {
		// First request: get the ETag
		req1 := httptest.NewRequest("GET", "/nodes/node-0", nil)
		req1.Header.Set("Accept", "application/json")
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)

		if w1.Code != http.StatusOK {
			t.Fatalf("first request status=%d, want 200", w1.Code)
		}
		etag := w1.Header().Get("ETag")
		if etag == "" {
			t.Fatal("expected ETag header on first request")
		}

		// Second request: send If-None-Match with the ETag
		req2 := httptest.NewRequest("GET", "/nodes/node-0", nil)
		req2.Header.Set("Accept", "application/json")
		req2.Header.Set("If-None-Match", etag)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		if w2.Code != http.StatusNotModified {
			t.Fatalf("conditional request status=%d, want 304", w2.Code)
		}
		if w2.Body.Len() != 0 {
			t.Errorf("304 response should have empty body, got %d bytes", w2.Body.Len())
		}
	})

	t.Run("discovery Link headers on JSON response", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nodes", nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		allLinks := strings.Join(w.Header().Values("Link"), ", ")
		if !strings.Contains(allLinks, `rel="service-desc"`) {
			t.Errorf("expected Link with rel=service-desc, got %q", allLinks)
		}
		if !strings.Contains(allLinks, `rel="describedby"`) {
			t.Errorf("expected Link with rel=describedby, got %q", allLinks)
		}
	})

	t.Run("discovery Link headers absent on meta endpoints", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/-/health", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Link headers from discoveryLinks should NOT be present for /-/ paths
		for _, val := range w.Header().Values("Link") {
			if strings.Contains(val, `rel="service-desc"`) {
				t.Errorf("meta endpoint should not have service-desc Link, got %q", val)
			}
			if strings.Contains(val, `rel="describedby"`) {
				t.Errorf("meta endpoint should not have describedby Link, got %q", val)
			}
		}
	})
}

func TestAPIDocEndpoints(t *testing.T) {
	router := setupIntegrationRouter(t)

	t.Run("GET /api with Accept text/html returns Scalar playground", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api", nil)
		req.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type=%q, want text/html", ct)
		}
		body := w.Body.String()
		if !strings.Contains(body, "<script") {
			t.Error("response body missing <script tag")
		}
		if !strings.Contains(body, "api-reference") {
			t.Error("response body missing api-reference identifier")
		}
		if !strings.Contains(body, "/api/scalar.js") {
			t.Error("response body should reference local /api/scalar.js, not CDN")
		}
	})

	t.Run("GET /api with Accept application/json returns OpenAPI spec", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api", nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type=%q, want application/json", ct)
		}
		// Should be valid JSON
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}
	})

	t.Run("GET /api with no Accept returns OpenAPI spec as JSON", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type=%q, want application/json for default negotiation", ct)
		}
	})

	t.Run("GET /api/context.jsonld returns JSON-LD context", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/context.jsonld", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if ct != "application/ld+json" {
			t.Fatalf("Content-Type=%q, want application/ld+json", ct)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}
		if body["@context"] == nil {
			t.Error("response missing @context key")
		}
	})

	t.Run("GET /api/scalar.js returns JavaScript", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/scalar.js", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if ct != "application/javascript" {
			t.Fatalf("Content-Type=%q, want application/javascript", ct)
		}
		if !strings.Contains(w.Body.String(), "scalar") {
			t.Error("response body does not contain expected scalar content")
		}
		cc := w.Header().Get("Cache-Control")
		if !strings.Contains(cc, "max-age=86400") {
			t.Errorf("Cache-Control=%q, want max-age=86400", cc)
		}
	})
}
