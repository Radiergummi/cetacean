package prometheus

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func noopErrorWriter(w http.ResponseWriter, _ *http.Request, _, detail string) {
	http.Error(w, detail, http.StatusBadRequest)
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

	proxy := NewProxy(prom.URL, noopErrorWriter)

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

	proxy := NewProxy(prom.URL, noopErrorWriter)

	req := httptest.NewRequest("GET", "/metrics?query=up&start=100&end=200&step=15", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestMetricsProxyHandler_MissingQuery(t *testing.T) {
	proxy := NewProxy("http://localhost:9090", noopErrorWriter)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetrics(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMetricsProxyHandler_NilProxy(t *testing.T) {
	nilProxyErrorWriter.Store(nil)
	defer SetNilProxyErrorWriter(noopErrorWriter)

	var proxy *Proxy

	req := httptest.NewRequest("GET", "/metrics?query=up", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetrics(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestMetricsProxyHandler_Labels(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/labels" {
			t.Errorf("expected /api/v1/labels, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":["__name__","instance","job"]}`))
	}))
	defer prom.Close()

	proxy := NewProxy(prom.URL, noopErrorWriter)
	req := httptest.NewRequest("GET", "/-/metrics/labels", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetricsLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestMetricsProxyHandler_LabelValues(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/label/job/values" {
			t.Errorf("expected /api/v1/label/job/values, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":["prometheus","node-exporter"]}`))
	}))
	defer prom.Close()

	proxy := NewProxy(prom.URL, noopErrorWriter)
	req := httptest.NewRequest("GET", "/-/metrics/labels/job", nil)
	req.SetPathValue("name", "job")
	w := httptest.NewRecorder()
	proxy.HandleMetricsLabelValues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestMetricsProxyHandler_LabelValues_NilProxy(t *testing.T) {
	nilProxyErrorWriter.Store(nil)
	defer SetNilProxyErrorWriter(noopErrorWriter)

	var proxy *Proxy
	req := httptest.NewRequest("GET", "/-/metrics/labels/job", nil)
	req.SetPathValue("name", "job")
	w := httptest.NewRecorder()
	proxy.HandleMetricsLabelValues(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", w.Code)
	}
}
