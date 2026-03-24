package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

const maxMetricsStreamClients = 64

var metricsStreamCount atomic.Int32

// testTickerInterval overrides the tick interval in tests (zero means use step duration).
var testTickerInterval time.Duration

func (h *Handlers) HandleMetricsStream(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if query == "" {
		writeErrorCode(w, r, "MTR003", "missing required parameter: query")
		return
	}

	if h.promClient == nil {
		writeErrorCode(w, r, "MTR001", "prometheus not configured")
		return
	}

	step := 15
	if s := r.URL.Query().Get("step"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 5 || v > 300 {
			writeErrorCode(w, r, "MTR004", "step must be between 5 and 300 seconds")
			return
		}
		step = v
	}

	rangeSec := 3600
	if rs := r.URL.Query().Get("range"); rs != "" {
		v, err := strconv.Atoi(rs)
		if err == nil && v > 0 {
			rangeSec = v
		}
	}

	for {
		cur := metricsStreamCount.Load()
		if int(cur) >= maxMetricsStreamClients {
			w.Header().Set("Retry-After", "5")
			writeErrorCode(w, r, "MTR005", "too many metrics stream connections")
			return
		}
		if metricsStreamCount.CompareAndSwap(cur, cur+1) {
			break
		}
	}
	defer metricsStreamCount.Add(-1)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorCode(w, r, "API005", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ctx := r.Context()

	now := strconv.FormatInt(time.Now().Unix(), 10)
	start := strconv.FormatInt(time.Now().Unix()-int64(rangeSec), 10)
	stepStr := strconv.Itoa(step)
	initial, err := h.promClient.RangeQueryRaw(ctx, query, start, now, stepStr)
	if err != nil {
		fmt.Fprintf(w, "event: query_error\ndata: %s\n\n", marshalErrorEvent(err))
		flusher.Flush()
	} else {
		writeSSEEvent(w, flusher, "initial", initial)
	}

	interval := time.Duration(step) * time.Second
	if testTickerInterval > 0 {
		interval = testTickerInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	type queryResult struct {
		data []byte
		err  error
	}
	results := make(chan queryResult, 1)
	var inflight atomic.Bool

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if inflight.Load() {
				continue
			}
			inflight.Store(true)
			go func() {
				raw, qErr := h.promClient.InstantQueryRaw(ctx, query)
				select {
				case results <- queryResult{raw, qErr}:
				case <-ctx.Done():
				}
				inflight.Store(false)
			}()
		case res := <-results:
			if ctx.Err() != nil {
				return
			}
			if res.err != nil {
				fmt.Fprintf(w, "event: query_error\ndata: %s\n\n", marshalErrorEvent(res.err))
				flusher.Flush()
			} else {
				writeSSEEvent(w, flusher, "point", res.data)
			}
			keepalive.Reset(15 * time.Second)
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}

func marshalErrorEvent(err error) string {
	msg := err.Error()
	b, _ := json.Marshal(map[string]string{"error": msg, "errorType": "server_error"})
	return string(b)
}
