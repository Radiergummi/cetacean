package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/docker/docker/api/types/swarm"

	"cetacean/internal/cache"
	"cetacean/internal/docker"
)

// mockLogStreamer returns pre-built Docker multiplex frames.
type mockLogStreamer struct {
	data []byte
	err  error
}

func (m *mockLogStreamer) Logs(_ context.Context, _ docker.LogKind, _ string, _ string, _ bool, _, _ string) (io.ReadCloser, error) {
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

	h := NewHandlers(c, &mockLogStreamer{data: frames.Bytes()}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/api/services/svc1/logs?limit=100", nil)
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
	h := NewHandlers(c, &mockLogStreamer{}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/api/services/missing/logs", nil)
	req.SetPathValue("id", "missing")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleServiceLogs_SSE(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z hello\n"))
	frames.Write(buildFrame(2, "2024-01-01T00:00:01.000000000Z world\n"))

	h := NewHandlers(c, &mockLogStreamer{data: frames.Bytes()}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/api/services/svc1/logs", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
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
	for _, raw := range strings.Split(events[0], "\n") {
		if strings.HasPrefix(raw, "data: ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(raw, "data: ")), &line); err != nil {
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

	h := NewHandlers(c, &mockLogStreamer{data: frames.Bytes()}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/api/tasks/t1/logs?limit=50", nil)
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

	h := NewHandlers(c, &mockLogStreamer{data: nil}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/api/services/svc1/logs", nil)
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

	h := NewHandlers(c, &mockLogStreamer{data: nil}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/api/services/svc1/logs", nil)
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

	h := NewHandlers(c, &mockLogStreamer{data: frames.Bytes()}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/api/services/svc1/logs", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
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

	h := NewHandlers(c, &mockLogStreamer{data: frames.Bytes()}, closedReady(), nil)

	// Filter to stderr only
	req := httptest.NewRequest("GET", "/api/services/svc1/logs?stream=stderr", nil)
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

	h := NewHandlers(c, &mockLogStreamer{data: frames.Bytes()}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/api/services/svc1/logs?stream=stdout", nil)
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
		onCall: func(since string) { capturedSince = since },
	}
	h := NewHandlers(c, mock, closedReady(), nil)

	// Simulate EventSource reconnect with Last-Event-ID header
	req := httptest.NewRequest("GET", "/api/services/svc1/logs", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Last-Event-ID", "2024-01-01T00:00:03.000000000Z")
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
		onCall: func(since string) { capturedSince = since },
	}
	h := NewHandlers(c, mock, closedReady(), nil)

	// Both ?after= and Last-Event-ID present — ?after= should win
	req := httptest.NewRequest("GET", "/api/services/svc1/logs?after=explicit-value", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Last-Event-ID", "header-value")
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	if capturedSince != "explicit-value" {
		t.Errorf("since = %q, want explicit ?after= value", capturedSince)
	}
}

// capturingLogStreamer captures the since param passed to log calls.
type capturingLogStreamer struct {
	data   []byte
	onCall func(since string)
}

func (m *capturingLogStreamer) Logs(_ context.Context, _ docker.LogKind, _ string, _ string, _ bool, since, _ string) (io.ReadCloser, error) {
	if m.onCall != nil {
		m.onCall(since)
	}
	return io.NopCloser(bytes.NewReader(m.data)), nil
}

func TestHandleServiceLogs_SSE_StreamFilter(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z stdout line\n"))
	frames.Write(buildFrame(2, "2024-01-01T00:00:01.000000000Z stderr line\n"))

	h := NewHandlers(c, &mockLogStreamer{data: frames.Bytes()}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/api/services/svc1/logs?stream=stderr", nil)
	req.SetPathValue("id", "svc1")
	req.Header.Set("Accept", "text/event-stream")
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
