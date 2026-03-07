package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrometheusProxy_ForwardsQuery(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "up" {
			t.Errorf("expected query=up, got %s", r.URL.Query().Get("query"))
		}
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer prom.Close()

	proxy := NewPrometheusProxy(prom.URL)

	req := httptest.NewRequest("GET", "/api/metrics/query?query=up", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != `{"status":"success"}` {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

func TestPrometheusProxy_ForwardsQueryRange(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer prom.Close()

	proxy := NewPrometheusProxy(prom.URL)

	req := httptest.NewRequest("GET", "/api/metrics/query_range?query=up&start=0&end=1&step=15", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestPrometheusProxy_BlocksForbiddenPath(t *testing.T) {
	proxy := NewPrometheusProxy("http://localhost:9090")

	req := httptest.NewRequest("GET", "/api/metrics/admin/tsdb/delete", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}
