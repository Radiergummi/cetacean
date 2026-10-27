package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/radiergummi/cetacean/internal/api/sse"
)

const maxMetricsStreamClients = 64

func (h *Handlers) HandleMetricsStream(w http.ResponseWriter, r *http.Request) {
	if !h.requireReadEverything(w, r) {
		return
	}

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

	if sse.ServeHead(w, r) {
		return
	}

	if !tryAcquire(&h.metricsStreamCount, maxMetricsStreamClients) {
		w.Header().Set("Retry-After", sse.RetryAfter())
		writeErrorCode(w, r, "MTR005", "too many metrics stream connections")
		return
	}
	defer h.metricsStreamCount.Add(-1)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorCode(w, r, "API005", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	r, release := h.streams.Bind(r)
	defer release()

	ctx := r.Context()

	now := strconv.FormatInt(time.Now().Unix(), 10)
	start := strconv.FormatInt(time.Now().Unix()-int64(rangeSec), 10)
	stepStr := strconv.Itoa(step)
	// The frame carries a fixed message; the cause is logged once per run of
	// failures rather than on every tick.
	failing := false
	writeQueryError := func(err error) {
		if !failing && ctx.Err() == nil {
			slog.Warn("metrics stream query failed", "query", query, "error", err)
		}
		failing = true
		fmt.Fprintf(w, "event: query_error\ndata: %s\n\n", marshalErrorEvent(err))
		flusher.Flush()
	}

	initial, err := h.promClient.RangeQueryRaw(ctx, query, start, now, stepStr)
	if err != nil {
		writeQueryError(err)
	} else {
		writeSSEEvent(w, flusher, "initial", initial)
	}

	interval := time.Duration(step) * time.Second
	if h.tickerInterval > 0 {
		interval = h.tickerInterval
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
				writeQueryError(res.err)
			} else {
				failing = false
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
