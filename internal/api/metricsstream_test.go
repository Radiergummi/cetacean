package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMetricsStream_MissingQuery(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest("GET", "/-/metrics/query_range", nil)
	w := httptest.NewRecorder()
	h.HandleMetricsStream(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestMetricsStream_NoPromClient(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up", nil)
	w := httptest.NewRecorder()
	h.HandleMetricsStream(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestMetricsStream_ConnectionLimit(t *testing.T) {
	metricsStreamCount.Store(int32(maxMetricsStreamClients))
	defer metricsStreamCount.Store(0)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	h := &Handlers{promClient: NewPromClient(srv.URL)}
	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up", nil)
	w := httptest.NewRecorder()
	h.HandleMetricsStream(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") != "5" {
		t.Error("expected Retry-After: 5 header")
	}
}

func TestMetricsStream_StreamsEvents(t *testing.T) {
	testTickerInterval = 10 * time.Millisecond
	defer func() { testTickerInterval = 0 }()

	queryCount := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/query_range" {
			w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		} else {
			w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		}
	}))
	defer srv.Close()

	h := &Handlers{promClient: NewPromClient(srv.URL)}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up&step=15", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleMetricsStream(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "event: initial") {
		t.Error("expected initial event in body")
	}
	if !strings.Contains(body, "event: point") {
		t.Error("expected at least one point event in body")
	}
	if queryCount.Load() < 2 {
		t.Errorf(
			"expected at least 2 Prometheus calls (range + instant), got %d",
			queryCount.Load(),
		)
	}
}

func TestMetricsStream_FullLifecycle(t *testing.T) {
	testTickerInterval = 20 * time.Millisecond
	defer func() { testTickerInterval = 0 }()

	queryCount := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		ts := time.Now().Unix()
		if r.URL.Path == "/api/v1/query_range" {
			fmt.Fprintf(
				w,
				`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"cpu"},"values":[[%d,"42"]]}]}}`,
				ts,
			)
		} else {
			fmt.Fprintf(
				w,
				`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"cpu"},"value":[%d,"43"]}]}}`,
				ts,
			)
		}
	}))
	defer srv.Close()

	h := &Handlers{promClient: NewPromClient(srv.URL)}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=cpu&step=15&range=300", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleMetricsStream(w, req)

	body := w.Body.String()

	if !strings.Contains(body, "event: initial") {
		t.Error("missing initial event")
	}
	if !strings.Contains(body, `"cpu"`) {
		t.Error("initial event should contain metric name")
	}
	if !strings.Contains(body, "event: point") {
		t.Error("missing point event")
	}
	if queryCount.Load() < 2 {
		t.Errorf("expected at least 2 Prometheus calls, got %d", queryCount.Load())
	}
	if metricsStreamCount.Load() != 0 {
		t.Error("connection counter should be 0 after handler returns")
	}
}

func TestMetricsStream_ErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	h := &Handlers{promClient: NewPromClient(srv.URL)}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up&step=15", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleMetricsStream(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "event: query_error") {
		t.Error("expected query_error event when Prometheus returns 500")
	}
	if !strings.Contains(body, "server_error") {
		t.Error("expected errorType in error event data")
	}
}
