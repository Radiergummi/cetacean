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

	req := httptest.NewRequest("GET", "/-/metrics/query?query=up", nil)
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

	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up&start=0&end=1&step=15", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestPrometheusProxy_BlocksForbiddenPath(t *testing.T) {
	proxy := NewPrometheusProxy("http://localhost:9090")

	req := httptest.NewRequest("GET", "/-/metrics/admin/tsdb/delete", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestPrometheusProxy_UpstreamFailure(t *testing.T) {
	// Use an invalid URL that will fail to connect
	proxy := NewPrometheusProxy("http://127.0.0.1:1")

	req := httptest.NewRequest("GET", "/-/metrics/query?query=up", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestMetricsProxyHandler_InstantQuery(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("expected /api/v1/query, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "up" {
			t.Errorf("expected query=up, got %s", r.URL.Query().Get("query"))
		}
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer prom.Close()

	proxy := NewPrometheusProxy(prom.URL)

	req := httptest.NewRequest("GET", "/metrics?query=up", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != `{"status":"success"}` {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

func TestMetricsProxyHandler_RangeQuery(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			t.Errorf("expected /api/v1/query_range, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "up" {
			t.Errorf("expected query=up, got %s", r.URL.Query().Get("query"))
		}
		if r.URL.Query().Get("start") != "100" {
			t.Errorf("expected start=100, got %s", r.URL.Query().Get("start"))
		}
		if r.URL.Query().Get("end") != "200" {
			t.Errorf("expected end=200, got %s", r.URL.Query().Get("end"))
		}
		if r.URL.Query().Get("step") != "15" {
			t.Errorf("expected step=15, got %s", r.URL.Query().Get("step"))
		}
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer prom.Close()

	proxy := NewPrometheusProxy(prom.URL)

	req := httptest.NewRequest("GET", "/metrics?query=up&start=100&end=200&step=15", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestMetricsProxyHandler_MissingQuery(t *testing.T) {
	proxy := NewPrometheusProxy("http://localhost:9090")

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetrics(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMetricsProxyHandler_NilProxy(t *testing.T) {
	var proxy *PrometheusProxy

	req := httptest.NewRequest("GET", "/metrics?query=up", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetrics(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
