package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/docker"
)

// mockLogStreamer returns pre-built Docker multiplex frames.
type mockLogStreamer struct {
	data []byte
	err  error
}

func (m *mockLogStreamer) Logs(
	_ context.Context,
	_ docker.LogKind,
	_ string,
	_ string,
	_ bool,
	_, _ string,
) (io.ReadCloser, error) {
	if m.err != nil {
		return nil, m.err
	}
	return io.NopCloser(bytes.NewReader(m.data)), nil
}

func TestHandleServiceLogs_JSON(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z line1\n"))
	frames.Write(buildFrame(2, "2024-01-01T00:00:01.000000000Z line2\n"))

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: frames.Bytes()}))

	req := httptest.NewRequest("GET", "/services/svc1/logs?limit=100", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}

	var resp LogResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(resp.Lines))
	}
	if resp.Lines[0].Stream != "stdout" {
		t.Errorf("lines[0].Stream = %q", resp.Lines[0].Stream)
	}
	if resp.Lines[1].Stream != "stderr" {
		t.Errorf("lines[1].Stream = %q", resp.Lines[1].Stream)
	}
	if resp.Oldest != "2024-01-01T00:00:00.000000000Z" {
		t.Errorf("oldest = %q", resp.Oldest)
	}
	if resp.Newest != "2024-01-01T00:00:01.000000000Z" {
		t.Errorf("newest = %q", resp.Newest)
	}
}

func TestHandleServiceLogs_JSON_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{}))

	req := httptest.NewRequest("GET", "/services/missing/logs", nil)
	req.SetPathValue("id", "missing")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func withContentType(r *http.Request, ct ContentType) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), contentTypeKey{}, ct))
}

func TestHandleServiceLogs_SSE(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z hello\n"))
	frames.Write(buildFrame(2, "2024-01-01T00:00:01.000000000Z world\n"))

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: frames.Bytes()}))

	req := httptest.NewRequest("GET", "/services/svc1/logs", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
	req = withContentType(req, ContentTypeSSE)
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}

	body := w.Body.String()
	// Each event is: id: <ts>\ndata: <json>\n\n
	events := strings.Split(strings.TrimSpace(body), "\n\n")
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2; body:\n%s", len(events), body)
	}

	// Parse first event — extract data line
	var line LogLine
	for raw := range strings.SplitSeq(events[0], "\n") {
		if after, ok := strings.CutPrefix(raw, "data: "); ok {
			if err := json.Unmarshal([]byte(after), &line); err != nil {
				t.Fatal(err)
			}
		}
	}
	if line.Message != "hello" {
		t.Errorf("message = %q", line.Message)
	}
	if line.Stream != "stdout" {
		t.Errorf("stream = %q", line.Stream)
	}
}

func TestHandleTaskLogs_JSON(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "t1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z task log\n"))

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: frames.Bytes()}))

	req := httptest.NewRequest("GET", "/tasks/t1/logs?limit=50", nil)
	req.SetPathValue("id", "t1")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	h.HandleTaskLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp LogResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(resp.Lines))
	}
	if resp.Lines[0].Message != "task log" {
		t.Errorf("message = %q", resp.Lines[0].Message)
	}
}

func TestHandleServiceLogs_JSON_Empty(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: nil}))

	req := httptest.NewRequest("GET", "/services/svc1/logs", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp LogResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Lines) != 0 {
		t.Fatalf("got %d lines, want 0", len(resp.Lines))
	}
}

func TestHandleServiceLogs_DefaultsToJSON(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: nil}))

	req := httptest.NewRequest("GET", "/services/svc1/logs", nil)
	req.SetPathValue("id", "svc1")
	// No Accept header
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

func TestHandleServiceLogs_SSE_EventIDs(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z line1\n"))
	frames.Write(buildFrame(1, "2024-01-01T00:00:01.000000000Z line2\n"))

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: frames.Bytes()}))

	req := httptest.NewRequest("GET", "/services/svc1/logs", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
	req = withContentType(req, ContentTypeSSE)
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	body := w.Body.String()
	// Each event should have an id: field with the timestamp
	events := strings.Split(strings.TrimSpace(body), "\n\n")
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2; body:\n%s", len(events), body)
	}

	// First event should have id: 2024-01-01T00:00:00.000000000Z
	if !strings.Contains(events[0], "id: 2024-01-01T00:00:00.000000000Z") {
		t.Errorf("event[0] missing id field:\n%s", events[0])
	}
	if !strings.Contains(events[1], "id: 2024-01-01T00:00:01.000000000Z") {
		t.Errorf("event[1] missing id field:\n%s", events[1])
	}
}

func TestHandleServiceLogs_JSON_StreamFilter(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z stdout line\n"))
	frames.Write(buildFrame(2, "2024-01-01T00:00:01.000000000Z stderr line\n"))
	frames.Write(buildFrame(1, "2024-01-01T00:00:02.000000000Z another stdout\n"))

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: frames.Bytes()}))

	// Filter to stderr only
	req := httptest.NewRequest("GET", "/services/svc1/logs?stream=stderr", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	var resp LogResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(resp.Lines))
	}
	if resp.Lines[0].Message != "stderr line" {
		t.Errorf("message = %q", resp.Lines[0].Message)
	}
}

func TestHandleServiceLogs_JSON_StreamFilterStdout(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z stdout line\n"))
	frames.Write(buildFrame(2, "2024-01-01T00:00:01.000000000Z stderr line\n"))

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: frames.Bytes()}))

	req := httptest.NewRequest("GET", "/services/svc1/logs?stream=stdout", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	var resp LogResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(resp.Lines))
	}
	if resp.Lines[0].Stream != "stdout" {
		t.Errorf("stream = %q", resp.Lines[0].Stream)
	}
}

func TestHandleServiceLogs_SSE_LastEventID(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:05.000000000Z after reconnect\n"))

	// Track what "since" value the mock receives
	var capturedSince string
	mock := &capturingLogStreamer{
		data:   frames.Bytes(),
		onCall: func(_, since string) { capturedSince = since },
	}
	h := newTestHandlers(t, withCache(c), withDockerClient(mock))

	// Simulate EventSource reconnect with Last-Event-ID header
	req := httptest.NewRequest("GET", "/services/svc1/logs", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Last-Event-ID", "2024-01-01T00:00:03.000000000Z")
	req = withContentType(req, ContentTypeSSE)
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	if capturedSince != "2024-01-01T00:00:03.000000000Z" {
		t.Errorf("since = %q, want Last-Event-ID value", capturedSince)
	}
}

func TestHandleServiceLogs_SSE_LastEventID_OverriddenByAfter(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var capturedSince string
	mock := &capturingLogStreamer{
		data:   buildFrame(1, "2024-01-01T00:00:05.000000000Z line\n"),
		onCall: func(_, since string) { capturedSince = since },
	}
	h := newTestHandlers(t, withCache(c), withDockerClient(mock))

	// Both ?after= and Last-Event-ID present — ?after= should win
	req := httptest.NewRequest("GET", "/services/svc1/logs?after=2024-01-01T00:00:01Z", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Last-Event-ID", "2024-01-01T00:00:00Z")
	req = withContentType(req, ContentTypeSSE)
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	if capturedSince != "2024-01-01T00:00:01Z" {
		t.Errorf("since = %q, want explicit ?after= value", capturedSince)
	}
}

// capturingLogStreamer captures the tail and since params passed to log calls.
type capturingLogStreamer struct {
	data   []byte
	onCall func(tail, since string)
}

func (m *capturingLogStreamer) Logs(
	_ context.Context,
	_ docker.LogKind,
	_ string,
	tail string,
	_ bool,
	since, _ string,
) (io.ReadCloser, error) {
	if m.onCall != nil {
		m.onCall(tail, since)
	}
	return io.NopCloser(bytes.NewReader(m.data)), nil
}

func TestHandleServiceLogs_SSE_StreamFilter(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z stdout line\n"))
	frames.Write(buildFrame(2, "2024-01-01T00:00:01.000000000Z stderr line\n"))

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: frames.Bytes()}))

	req := httptest.NewRequest("GET", "/services/svc1/logs?stream=stderr", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
	req = withContentType(req, ContentTypeSSE)
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	body := w.Body.String()
	events := strings.Split(strings.TrimSpace(body), "\n\n")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1; body:\n%s", len(events), body)
	}
	if !strings.Contains(events[0], "stderr line") {
		t.Errorf("expected stderr line in event:\n%s", events[0])
	}
}

// A client reconnecting a dropped stream passes the last line it saw as
// ?after=. Docker ignores since for service logs, so the handler has to
// enforce the cursor itself — otherwise every reconnect silently drops the
// lines emitted while the client was away.
func TestHandleServiceLogs_SSE_ReplaysLinesAfterCursor(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z before\n"))
	frames.Write(buildFrame(1, "2024-01-01T00:00:01.000000000Z at cursor\n"))
	frames.Write(buildFrame(1, "2024-01-01T00:00:02.000000000Z missed\n"))
	frames.Write(buildFrame(1, "2024-01-01T00:00:03.000000000Z also missed\n"))

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: frames.Bytes()}))

	req := httptest.NewRequest(
		"GET",
		"/services/svc1/logs?after=2024-01-01T00:00:01.000000000Z",
		nil,
	)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
	req = withContentType(req, ContentTypeSSE)
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	body := w.Body.String()
	for _, want := range []string{"missed", "also missed"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q line emitted after the cursor:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"before", "at cursor"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("body replayed %q, which is at or before the cursor:\n%s", unwanted, body)
		}
	}
}

func TestHandleServiceLogs_SSE_RequestsBacklogOnlyWhenResuming(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	cases := []struct {
		name     string
		target   string
		wantTail string
	}{
		{
			name:     "fresh stream takes no history",
			target:   "/services/svc1/logs",
			wantTail: "0",
		},
		{
			name:     "resumed stream asks for a bounded backlog",
			target:   "/services/svc1/logs?after=2024-01-01T00:00:01Z",
			wantTail: strconv.Itoa(logResumeTail),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedTail string
			mock := &capturingLogStreamer{
				onCall: func(tail, _ string) { capturedTail = tail },
			}
			h := newTestHandlers(t, withCache(c), withDockerClient(mock))

			req := httptest.NewRequest("GET", tc.target, nil)
			req.SetPathValue("id", "svc1")
			req.Header.Set("Accept", "text/event-stream")
			req = withContentType(req, ContentTypeSSE)
			h.HandleServiceLogs(httptest.NewRecorder(), req)

			if capturedTail != tc.wantTail {
				t.Errorf("tail = %q, want %q", capturedTail, tc.wantTail)
			}
		})
	}
}

// buildFrame writes one Docker multiplexed log frame. The logs package has its
// own copy; Go test helpers do not cross package boundaries.
func buildFrame(streamType byte, payload string) []byte {
	header := make([]byte, 8)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))

	return append(header, []byte(payload)...)
}

// TestHandleServiceLogs_JSON_AfterKeepsUntimestampedLines pins a deliberate
// behaviour change: the old since/until compaction dropped lines with no
// timestamp whenever ?after= was set (an empty timestamp compares <= any
// non-empty cursor). logs.FilterSince keeps them instead, since a line with
// no timestamp can't be placed relative to the cursor and dropping it would
// silently lose output. This test pins that as intentional.
func TestHandleServiceLogs_JSON_AfterKeepsUntimestampedLines(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z before\n"))
	frames.Write(buildFrame(1, "no timestamp survives\n"))
	frames.Write(buildFrame(1, "2024-01-01T00:00:02.000000000Z after\n"))

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: frames.Bytes()}))

	req := httptest.NewRequest(
		"GET",
		"/services/svc1/logs?after=2024-01-01T00:00:01.000000000Z&limit=100",
		nil,
	)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp LogResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.Lines) != 2 {
		t.Fatalf(
			"got %d lines, want 2 (untimestamped + after cursor): %+v",
			len(resp.Lines),
			resp.Lines,
		)
	}

	var sawUntimestamped, sawBefore bool
	for _, l := range resp.Lines {
		if l.Message == "no timestamp survives" {
			sawUntimestamped = true
		}
		if l.Message == "before" {
			sawBefore = true
		}
	}
	if !sawUntimestamped {
		t.Errorf("lines = %+v, want the untimestamped line to survive", resp.Lines)
	}
	if sawBefore {
		t.Errorf("lines = %+v, want the line before the cursor filtered out", resp.Lines)
	}
}

// TestHandleServiceLogs_SSE_AfterKeepsUntimestampedLines pins the SSE path to
// the same cursor semantic as logs.FilterSince: a line with no timestamp
// cannot be placed relative to the cursor, so it survives a resumed stream.
// A log frame containing embedded newlines yields exactly such continuation
// lines — dropping them would make an automatic reconnect silently change
// what the viewer displays.
func TestHandleServiceLogs_SSE_AfterKeepsUntimestampedLines(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z before\n"))
	frames.Write(buildFrame(1, "2024-01-01T00:00:02.000000000Z after\n"))
	frames.Write(buildFrame(1, "no timestamp survives\n"))

	h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{data: frames.Bytes()}))

	req := httptest.NewRequest(
		"GET",
		"/services/svc1/logs?after=2024-01-01T00:00:01.000000000Z",
		nil,
	)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
	req = withContentType(req, ContentTypeSSE)
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "no timestamp survives") {
		t.Errorf("resumed stream dropped the untimestamped line:\n%s", body)
	}
	if strings.Contains(body, "before") {
		t.Errorf("resumed stream replayed a line at or before the cursor:\n%s", body)
	}
}
