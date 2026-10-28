package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/api/sse"
	promapi "github.com/radiergummi/cetacean/internal/prometheus"
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	h := &Handlers{promClient: promapi.NewClient(srv.URL)}
	h.metricsStreamCount.Store(maxMetricsStreamClients)
	defer h.metricsStreamCount.Store(0)
	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up", nil)
	w := httptest.NewRecorder()
	h.HandleMetricsStream(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	assertRetryAfter(t, w.Header().Get("Retry-After"))
}

func TestMetricsStream_HeadAsksPrometheusNothing(t *testing.T) {
	var queries atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		queries.Add(1)
	}))
	defer srv.Close()

	h := &Handlers{promClient: promapi.NewClient(srv.URL)}
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.HandleMetricsStream(w, httptest.NewRequest(http.MethodHead, "/metrics?query=up", nil))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HEAD on the metrics stream did not return")
	}

	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("HEAD answered %d with Content-Type %q, want 200 text/event-stream",
			w.Code, w.Header().Get("Content-Type"))
	}
	if n := queries.Load(); n != 0 || h.metricsStreamCount.Load() != 0 {
		t.Errorf("HEAD made %d queries and left %d streams counted",
			n, h.metricsStreamCount.Load())
	}
}

func TestMetricsStream_StreamsEvents(t *testing.T) {
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

	h := &Handlers{
		promClient:     promapi.NewClient(srv.URL),
		tickerInterval: 10 * time.Millisecond,
	}

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
	if strings.Contains(body, "\nid: ") {
		t.Error("metrics frames carry no id; this stream writes none")
	}
	if queryCount.Load() < 2 {
		t.Errorf(
			"expected at least 2 Prometheus calls (range + instant), got %d",
			queryCount.Load(),
		)
	}
}

func TestMetricsStream_FullLifecycle(t *testing.T) {
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

	h := &Handlers{
		promClient:     promapi.NewClient(srv.URL),
		tickerInterval: 20 * time.Millisecond,
	}

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
	if h.metricsStreamCount.Load() != 0 {
		t.Error("connection counter should be 0 after handler returns")
	}
}

func TestMetricsStream_ErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	h := &Handlers{promClient: promapi.NewClient(srv.URL)}
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
	if strings.Contains(body, "\nid: ") {
		t.Error("metrics frames carry no id; this stream writes none")
	}
}

// The query_error frame omits the cause, so the log must hold it — once per
// failure, not once per tick.
func TestMetricsStream_LogsTheCauseOnceWhenQueriesStartFailing(t *testing.T) {
	refused := httptest.NewServer(http.NotFoundHandler())
	refused.Close()

	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(previous)

	h := &Handlers{
		promClient:     promapi.NewClient(refused.URL),
		tickerInterval: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up&step=15", nil)
	w := httptest.NewRecorder()
	h.HandleMetricsStream(w, req.WithContext(ctx))

	if n := strings.Count(w.Body.String(), "event: query_error"); n < 2 {
		t.Fatalf("got %d query_error frames, want several", n)
	}
	if n := strings.Count(logged.String(), "connection refused"); n != 1 {
		t.Errorf("logged the cause %d times, want once:\n%s", n, logged.String())
	}
}

// The log tail refuses past its cap with the same spread Retry-After the other
// capped streams send.
func TestLogStreamConnectionLimit(t *testing.T) {
	h := &Handlers{}
	occupy(t, &h.logStreams, maxLogSSEConns, maxLogSSEConns)

	w := httptest.NewRecorder()
	h.serveLogsSSE(w, httptest.NewRequest("GET", "/services/x/logs", nil), nil, "", "")

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", w.Code)
	}
	assertRetryAfter(t, w.Header().Get("Retry-After"))
}

// assertRetryAfter fails unless value is one sse.RetryAfter could have produced.
func assertRetryAfter(t *testing.T, value string) {
	t.Helper()

	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < sse.RetryAfterFloor ||
		seconds >= sse.RetryAfterFloor+sse.RetryAfterWindow {
		t.Errorf("Retry-After = %q, want delta-seconds in [%d, %d)",
			value, sse.RetryAfterFloor, sse.RetryAfterFloor+sse.RetryAfterWindow)
	}
}

// Shutdown waits for every handler; a metrics stream whose client stays must
// still end when the server stops.
func TestMetricsStream_EndsAtShutdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer srv.Close()

	lifetime := sse.NewLifetime()
	h := &Handlers{
		promClient:     promapi.NewClient(srv.URL),
		tickerInterval: 10 * time.Millisecond,
		streams:        lifetime,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up&step=15", nil)
		h.HandleMetricsStream(httptest.NewRecorder(), req)
	}()

	time.Sleep(50 * time.Millisecond)
	lifetime.End()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the metrics stream outlived shutdown")
	}
}

// As the metrics stream: a log follow ends at shutdown, not when its client leaves.
func TestLogFollow_EndsAtShutdown(t *testing.T) {
	lifetime := sse.NewLifetime()
	h := &Handlers{streams: lifetime}

	pipeReader, pipeWriter := io.Pipe()
	defer pipeWriter.Close()

	fetch := func(ctx context.Context, _ string, _ bool, _, _ string) (LogStream, error) {
		context.AfterFunc(ctx, func() { pipeWriter.CloseWithError(ctx.Err()) })
		return pipeReader, nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.serveLogsSSE(
			httptest.NewRecorder(),
			httptest.NewRequest("GET", "/services/x/logs", nil),
			fetch,
			"",
			"",
		)
	}()

	time.Sleep(50 * time.Millisecond)
	lifetime.End()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the log follow outlived shutdown")
	}
}
