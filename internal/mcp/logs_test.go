package mcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/logs"
)

// buildLogFrame mirrors the Docker multiplexed log frame format used by
// logs.ParseDockerLogs: [stream(1)][padding(3)][size(4 BE)][payload].
func buildLogFrame(stream byte, payload string) []byte {
	var buf bytes.Buffer
	buf.WriteByte(stream)
	buf.Write([]byte{0, 0, 0})
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(payload)))
	buf.WriteString(payload)
	return buf.Bytes()
}

type fakeLogStreamer struct {
	frames []byte
	err    error
	// captured call args
	calledID    string
	calledTail  string
	calledSince string
}

func (f *fakeLogStreamer) Logs(
	_ context.Context,
	_ docker.LogKind,
	id, tail string,
	_ bool,
	since, _ string,
) (io.ReadCloser, error) {
	f.calledID = id
	f.calledTail = tail
	f.calledSince = since
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(bytes.NewReader(f.frames)), nil
}

func newLogTestServer(t *testing.T, c *cache.Cache, logs LogStreamer) *Server {
	t.Helper()
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	srv, err := New(c, Options{
		Config:         cfg,
		GlobalOpsLevel: config.OpsReadOnly,
		Logs:           logs,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv
}

func TestReadServiceLogsReturnsParsedLines(t *testing.T) {
	c := cache.New(nil)
	streamer := &fakeLogStreamer{
		frames: append(
			buildLogFrame(1, "2024-01-01T00:00:00.000000000Z line 1\n"),
			buildLogFrame(2, "2024-01-01T00:00:01.000000000Z line 2\n")...,
		),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readServiceLogs(context.Background(), "svc1")
	if err != nil {
		t.Fatalf("readServiceLogs: %v", err)
	}
	resp, ok := got.(LogResourceResponse)
	if !ok {
		t.Fatalf("readServiceLogs returned %T, want LogResourceResponse", got)
	}
	if len(resp.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(resp.Lines))
	}
	if resp.Cursor == "" {
		t.Error("cursor should be populated when lines present")
	}
	if streamer.calledID != "svc1" {
		t.Errorf("streamer called with id=%q, want svc1", streamer.calledID)
	}
}

func TestReadServiceLogsWithoutStreamerReturnsEmpty(t *testing.T) {
	c := cache.New(nil)
	srv := newLogTestServer(t, c, nil)

	got, err := srv.readServiceLogs(context.Background(), "svc1")
	if err != nil {
		t.Fatalf("readServiceLogs: %v", err)
	}
	resp, ok := got.(LogResourceResponse)
	if !ok {
		t.Fatalf("returned %T, want LogResourceResponse", got)
	}
	if len(resp.Lines) != 0 {
		t.Errorf("expected zero lines, got %d", len(resp.Lines))
	}
}

func TestReadServiceLogsPropagatesStreamerError(t *testing.T) {
	c := cache.New(nil)
	streamer := &fakeLogStreamer{err: errors.New("docker unavailable")}
	srv := newLogTestServer(t, c, streamer)

	_, err := srv.readServiceLogs(context.Background(), "svc1")
	if err == nil || !strings.Contains(err.Error(), "docker unavailable") {
		t.Fatalf("expected docker-unavailable error, got %v", err)
	}
}

// The cursor is the newest returned timestamp verbatim, at fixed nanosecond
// width. It used to be advanced by a nanosecond and formatted with
// RFC3339Nano, which trims trailing zeros — so a cursor taken from a line at
// .999999999Z came back as a bare second and sorted above the whole of that
// second, discarding it on the next read. Filtering excludes the boundary
// line itself, so no advance is needed.
func TestReadServiceLogsCursorKeepsFullPrecision(t *testing.T) {
	c := cache.New(nil)
	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2024-01-01T00:00:57.999999999Z last\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readServiceLogsImpl(context.Background(), "svc1", logOptions{})
	if err != nil {
		t.Fatalf("readServiceLogsImpl: %v", err)
	}

	if got.Cursor != "2024-01-01T00:00:57.999999999Z" {
		t.Errorf("cursor = %q, want the newest timestamp at full precision", got.Cursor)
	}
}

// Docker interleaves the output of a service's tasks, so the last line to
// arrive is not necessarily the newest. Taking its timestamp as the cursor
// moved the cursor backwards and dropped everything in between on the next
// read.
func TestReadServiceLogsCursorIsTheNewestNotTheLastToArrive(t *testing.T) {
	c := cache.New(nil)
	streamer := &fakeLogStreamer{
		frames: append(
			buildLogFrame(1, "2024-01-01T00:00:05.000000000Z from task a\n"),
			buildLogFrame(1, "2024-01-01T00:00:03.000000000Z from task b\n")...,
		),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readServiceLogsImpl(context.Background(), "svc1", logOptions{})
	if err != nil {
		t.Fatalf("readServiceLogsImpl: %v", err)
	}

	if got.Cursor != "2024-01-01T00:00:05.000000000Z" {
		t.Errorf("cursor = %q, want the newest timestamp seen", got.Cursor)
	}
	if got.Lines[0].Message != "from task b" {
		t.Errorf("lines = %+v, want them sorted oldest first", got.Lines)
	}
}

// The widened fetch a cursored read needs is an implementation detail: the
// caller asked for `tail` lines and the schema documents that as the number
// returned.
func TestReadServiceLogsTruncatesToRequestedTail(t *testing.T) {
	c := cache.New(nil)

	var frames []byte
	for i := range 30 {
		frames = append(
			frames,
			buildLogFrame(1, fmt.Sprintf("2024-01-01T00:00:%02d.000000000Z line\n", i))...,
		)
	}

	srv := newLogTestServer(t, c, &fakeLogStreamer{frames: frames})

	got, err := srv.readServiceLogsImpl(context.Background(), "svc1", logOptions{
		tail:  5,
		since: "2024-01-01T00:00:00.000000000Z",
	})
	if err != nil {
		t.Fatalf("readServiceLogsImpl: %v", err)
	}

	if len(got.Lines) != 5 {
		t.Errorf("got %d lines, want at most the requested tail of 5", len(got.Lines))
	}
}

func TestFilterLogLinesByLevel(t *testing.T) {
	lines := []logs.LogLine{
		{Message: "DEBUG something happened"},
		{Message: "INFO request handled"},
		{Message: "WARN slow response"},
		{Message: "ERROR boom"},
		{Message: "FATAL crashed"},
	}
	got := filterLogLines(lines, "warn")
	if len(got) != 3 {
		t.Errorf("warn filter kept %d lines, want 3", len(got))
	}
	got = filterLogLines(lines, "fatal")
	if len(got) != 1 || !strings.Contains(got[0].Message, "FATAL") {
		t.Errorf("fatal filter = %v, want only the FATAL line", got)
	}
	got = filterLogLines(lines, "")
	if len(got) != len(lines) {
		t.Errorf("empty filter dropped lines: kept %d of %d", len(got), len(lines))
	}
}

func TestGetLogsToolReturnsJSONResponse(t *testing.T) {
	c := cache.New(nil)
	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2024-01-01T00:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	td, ok := srv.findTool("get_logs")
	if !ok {
		t.Fatal("get_logs not registered")
	}
	out, err := td.handler(context.Background(), newCallToolRequest("get_logs", map[string]any{
		"service": "svc1",
		"tail":    float64(50),
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var resp LogResourceResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, out)
	}
	if len(resp.Lines) != 1 {
		t.Errorf("lines = %d, want 1", len(resp.Lines))
	}
	if streamer.calledTail != "50" {
		t.Errorf("streamer tail = %q, want 50", streamer.calledTail)
	}
}

func TestGetLogsToolRequiresService(t *testing.T) {
	c := cache.New(nil)
	srv := newLogTestServer(t, c, &fakeLogStreamer{})
	td, _ := srv.findTool("get_logs")

	_, err := td.handler(context.Background(), newCallToolRequest("get_logs", map[string]any{}))
	if err == nil {
		t.Fatal("expected error when service arg missing")
	}
}

// The get_logs cursor is only meaningful if passing it back returns newer
// lines. Docker ignores Since for service logs, so the filter has to happen
// here — without it every call returns the same newest lines and an agent
// paging through a log loops on the same output.
func TestReadServiceLogsHonoursCursor(t *testing.T) {
	c := cache.New(nil)
	streamer := &fakeLogStreamer{
		frames: append(
			append(
				buildLogFrame(1, "2024-01-01T00:00:00.000000000Z before\n"),
				buildLogFrame(1, "2024-01-01T00:00:01.000000000Z at cursor\n")...,
			),
			buildLogFrame(1, "2024-01-01T00:00:02.000000000Z after\n")...,
		),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readServiceLogsImpl(
		context.Background(),
		"svc1",
		logOptions{since: "2024-01-01T00:00:01.000000000Z"},
	)
	if err != nil {
		t.Fatalf("readServiceLogsImpl: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Fatalf("got %d lines, want only the one after the cursor: %+v", len(got.Lines), got.Lines)
	}
	if !strings.Contains(got.Lines[0].Message, "after") {
		t.Errorf("line = %q, want the line after the cursor", got.Lines[0].Message)
	}
}

// nextCursor must skip lines with no timestamp when picking what to advance:
// parseLine can leave Timestamp empty for a line that doesn't match the
// expected shape, and nextCursor("") returns "". If the last returned line
// happened to be one of those, the cursor would go empty, the client's next
// call would send no since, and paging would loop on the newest lines again
// — the exact bug this task exists to fix, just reintroduced from the other
// end.
func TestReadServiceLogsCursorSkipsUntimestampedTrailingLine(t *testing.T) {
	c := cache.New(nil)
	streamer := &fakeLogStreamer{
		frames: append(
			buildLogFrame(1, "2024-01-01T00:00:00.000000000Z first\n"),
			buildLogFrame(1, "no timestamp last\n")...,
		),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readServiceLogsImpl(context.Background(), "svc1", logOptions{})
	if err != nil {
		t.Fatalf("readServiceLogsImpl: %v", err)
	}

	if len(got.Lines) != 2 {
		t.Fatalf("got %d lines, want both returned: %+v", len(got.Lines), got.Lines)
	}

	want := "2024-01-01T00:00:00.000000000Z"
	if got.Cursor != want {
		t.Errorf("cursor = %q, want %q (taken from the last timestamped line)", got.Cursor, want)
	}
}

// When no returned line carries a timestamp at all, the cursor must fall
// back to the one the caller passed in rather than going empty — an empty
// cursor tells the client to start over at the newest lines, losing its
// place.
func TestReadServiceLogsCursorFallsBackToIncomingSinceWhenNoneTimestamped(t *testing.T) {
	c := cache.New(nil)
	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "no timestamp only\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readServiceLogsImpl(
		context.Background(),
		"svc1",
		logOptions{since: "2024-01-01T00:00:00.000000000Z"},
	)
	if err != nil {
		t.Fatalf("readServiceLogsImpl: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Fatalf("got %d lines, want 1: %+v", len(got.Lines), got.Lines)
	}
	if got.Cursor != "2024-01-01T00:00:00.000000000Z" {
		t.Errorf("cursor = %q, want the incoming since unchanged", got.Cursor)
	}
}

// A pathological tail must never reach Docker. The since-widening multiply
// used to run before the clamp, so a huge tail overflowed to a negative
// number that slipped past both min() and the upper bound — and moby reads a
// negative tail as "every line ever logged".
func TestReadServiceLogsClampsPathologicalTail(t *testing.T) {
	c := cache.New(nil)
	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2024-01-01T00:00:02.000000000Z line\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	_, err := srv.readServiceLogsImpl(context.Background(), "svc1", logOptions{
		tail:  math.MaxInt,
		since: "2024-01-01T00:00:01.000000000Z",
	})
	if err != nil {
		t.Fatalf("readServiceLogsImpl: %v", err)
	}

	tail, err := strconv.Atoi(streamer.calledTail)
	if err != nil {
		t.Fatalf("streamer tail %q is not a number: %v", streamer.calledTail, err)
	}
	if tail <= 0 || tail > maxLogTail {
		t.Errorf("streamer tail = %d, want a positive value no larger than %d", tail, maxLogTail)
	}
}

// A cursor the server cannot compare is worse than no cursor: every line
// filters out, the response echoes the same unusable value back, and the
// client pages forever on an empty result. Reject it up front instead.
func TestReadServiceLogsRejectsUnparseableSince(t *testing.T) {
	c := cache.New(nil)
	srv := newLogTestServer(t, c, &fakeLogStreamer{})

	_, err := srv.readServiceLogsImpl(context.Background(), "svc1", logOptions{since: "5m"})
	if err == nil {
		t.Fatal("expected an error for a since value that is not an RFC 3339 timestamp")
	}
	if !strings.Contains(err.Error(), "since") {
		t.Errorf("error = %q, want it to name the offending argument", err)
	}
}
