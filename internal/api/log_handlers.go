package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

// LogStream is an alias for the io.ReadCloser returned by Docker log APIs.
type LogStream = io.ReadCloser

// LogResponse is the JSON response for paginated log fetches.
type LogResponse struct {
	Lines   []LogLine `json:"lines"`
	Oldest  string    `json:"oldest"`
	Newest  string    `json:"newest"`
	HasMore bool      `json:"hasMore"`
}

type logFetcher func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error)

func validLogTimestamp(s string) bool {
	if s == "" {
		return true
	}
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return true
	}
	if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return true
	}
	if _, err := time.ParseDuration(s); err == nil {
		return true
	}
	return false
}

func (h *Handlers) serveLogs(w http.ResponseWriter, r *http.Request, fetch logFetcher) {
	q := r.URL.Query()
	since := q.Get("after")
	until := q.Get("before")
	streamFilter := q.Get("stream") // "", "stdout", or "stderr"
	if streamFilter != "" && streamFilter != "stdout" && streamFilter != "stderr" {
		writeErrorCode(w, r, "LOG002", `invalid "stream" parameter: must be "stdout" or "stderr"`)
		return
	}

	if !validLogTimestamp(since) {
		writeErrorCode(
			w,
			r,
			"LOG003",
			`invalid "after" parameter: must be RFC3339 timestamp or Go duration`,
		)
		return
	}
	if !validLogTimestamp(until) {
		writeErrorCode(
			w,
			r,
			"LOG004",
			`invalid "before" parameter: must be RFC3339 timestamp or Go duration`,
		)
		return
	}

	if ContentTypeFromContext(r.Context()) == ContentTypeSSE {
		if until != "" {
			writeErrorCode(
				w,
				r,
				"LOG005",
				`"before" parameter is not supported for SSE log streams`,
			)
			return
		}
		h.serveLogsSSE(w, r, fetch, since, streamFilter)
		return
	}

	limit := defaultLogLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}

	// Docker ignores since/until for service logs, so we request more lines
	// than needed and filter in Go. When paginating (since or until is set),
	// use a larger tail to ensure we fetch enough lines beyond the cursor.
	tail := limit
	if since != "" || until != "" {
		tail = min(limit*10, maxLogLimit)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	logs, err := fetch(ctx, strconv.Itoa(tail), false, since, until)
	if err != nil {
		slog.Error("failed to get logs", "error", err)
		writeErrorCode(w, r, "LOG006", "failed to get logs")
		return
	}
	defer logs.Close() //nolint:errcheck

	// Docker's ServiceLogs with Follow=false may not close the stream
	// after sending all data. Use an idle cancel: once we've received
	// some data, if no new data arrives within 2s, cancel the context
	// so the blocked read unblocks immediately.
	lines, err := ParseDockerLogsWithIdleCancel(logs, cancel, 2*time.Second)
	if err != nil {
		slog.Error("failed to parse logs", "error", err)
		writeErrorCode(w, r, "LOG007", "failed to parse logs")
		return
	}
	if lines == nil {
		lines = []LogLine{}
	}

	// Docker interleaves lines from multiple tasks; sort by timestamp so
	// truncation keeps the truly newest lines.
	slices.SortStableFunc(lines, func(a, b LogLine) int {
		return strings.Compare(a.Timestamp, b.Timestamp)
	})

	// Docker ignores since/until for service logs, so enforce them here.
	if since != "" || until != "" {
		filtered := lines[:0]
		for _, l := range lines {
			if since != "" && l.Timestamp <= since {
				continue
			}
			if until != "" && l.Timestamp >= until {
				continue
			}
			filtered = append(filtered, l)
		}
		lines = filtered
	}

	// Apply stream filter before truncation so hasMore is accurate.
	if streamFilter != "" {
		filtered := lines[:0]
		for _, l := range lines {
			if l.Stream == streamFilter {
				filtered = append(filtered, l)
			}
		}
		lines = filtered
	}

	// Docker's tail=N applies per task for service logs, so the total may
	// exceed the requested limit. Truncate to the last `limit` lines.
	hasMore := len(lines) > limit
	if hasMore {
		lines = lines[len(lines)-limit:]
	}

	resp := LogResponse{Lines: lines, HasMore: hasMore}
	if len(lines) > 0 {
		resp.Oldest = lines[0].Timestamp
		resp.Newest = lines[len(lines)-1].Timestamp
	}
	writeJSON(w, resp)
}

func (h *Handlers) serveLogsSSE(
	w http.ResponseWriter,
	r *http.Request,
	fetch logFetcher,
	since, streamFilter string,
) {
	for {
		cur := h.activeLogSSEConns.Load()
		if cur >= maxLogSSEConns {
			w.Header().Set("Retry-After", "5")
			writeErrorCode(w, r, "LOG001", "too many active log streams")
			return
		}
		if h.activeLogSSEConns.CompareAndSwap(cur, cur+1) {
			break
		}
	}
	defer h.activeLogSSEConns.Add(-1)

	// EventSource sends Last-Event-ID on reconnect; use it as fallback for since
	if since == "" {
		if v := r.Header.Get("Last-Event-ID"); validLogTimestamp(v) {
			since = v
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorCode(w, r, "API005", "streaming not supported")
		return
	}

	logs, err := fetch(r.Context(), "0", true, since, "")
	if err != nil {
		slog.Error("failed to stream logs", "error", err)
		writeErrorCode(w, r, "LOG008", "failed to stream logs")
		return
	}
	defer logs.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	ch := make(chan LogLine, 64)
	done := make(chan error, 1)
	go func() {
		done <- StreamDockerLogs(logs, ch)
		close(ch)
	}()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case line, ok := <-ch:
			if !ok {
				<-done
				return
			}
			if streamFilter != "" && line.Stream != streamFilter {
				continue
			}
			data, _ := json.Marshal(line)
			if line.Timestamp != "" {
				fmt.Fprintf(w, "id: %s\n", line.Timestamp)
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			logs.Close() // unblocks StreamDockerLogs's io.Read
			for range ch {
			}
			<-done
			return
		}
	}
}
