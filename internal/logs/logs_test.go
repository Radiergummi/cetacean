package logs

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

// buildFrame creates a Docker multiplex frame: [stream_type(1)][0(3)][size(4 big-endian)][payload]
func buildFrame(streamType byte, payload string) []byte {
	var buf bytes.Buffer
	buf.WriteByte(streamType)
	buf.Write([]byte{0, 0, 0})
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(payload)))
	buf.WriteString(payload)
	return buf.Bytes()
}

func TestParseDockerLogs_SingleStdoutLine(t *testing.T) {
	data := buildFrame(1, "2024-01-01T00:00:00.000000000Z hello world\n")
	lines, err := ParseDockerLogs(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].Timestamp != "2024-01-01T00:00:00.000000000Z" {
		t.Errorf("timestamp = %q", lines[0].Timestamp)
	}
	if lines[0].Message != "hello world" {
		t.Errorf("message = %q", lines[0].Message)
	}
	if lines[0].Stream != "stdout" {
		t.Errorf("stream = %q", lines[0].Stream)
	}
}

func TestParseDockerLogs_StderrStream(t *testing.T) {
	data := buildFrame(2, "2024-01-01T00:00:00.000000000Z error msg\n")
	lines, err := ParseDockerLogs(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].Stream != "stderr" {
		t.Errorf("stream = %q, want stderr", lines[0].Stream)
	}
}

func TestParseDockerLogs_MultipleFrames(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z line1\n"))
	buf.Write(buildFrame(2, "2024-01-01T00:00:01.000000000Z line2\n"))
	buf.Write(buildFrame(1, "2024-01-01T00:00:02.000000000Z line3\n"))

	lines, err := ParseDockerLogs(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if lines[1].Stream != "stderr" {
		t.Errorf("lines[1].Stream = %q, want stderr", lines[1].Stream)
	}
}

func TestParseDockerLogs_MultilineSingleFrame(t *testing.T) {
	// A single frame can contain multiple newline-separated lines
	data := buildFrame(
		1,
		"2024-01-01T00:00:00.000000000Z line1\n2024-01-01T00:00:01.000000000Z line2\n",
	)
	lines, err := ParseDockerLogs(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].Message != "line1" {
		t.Errorf("lines[0].Message = %q", lines[0].Message)
	}
	if lines[1].Message != "line2" {
		t.Errorf("lines[1].Message = %q", lines[1].Message)
	}
}

func TestParseDockerLogs_NoTimestamp(t *testing.T) {
	data := buildFrame(1, "no timestamp here\n")
	lines, err := ParseDockerLogs(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].Timestamp != "" {
		t.Errorf("timestamp = %q, want empty", lines[0].Timestamp)
	}
	if lines[0].Message != "no timestamp here" {
		t.Errorf("message = %q", lines[0].Message)
	}
}

func TestParseDockerLogs_WithDetails(t *testing.T) {
	data := buildFrame(
		1,
		"2024-01-01T00:00:00.000000000Z com.docker.swarm.node.id=n1,com.docker.swarm.service.id=s1,com.docker.swarm.task.id=t1 hello\n",
	)
	lines, err := ParseDockerLogs(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].Message != "hello" {
		t.Errorf("message = %q, want hello", lines[0].Message)
	}
	if lines[0].Attrs["taskId"] != "t1" {
		t.Errorf("taskId = %q, want t1", lines[0].Attrs["taskId"])
	}
	if lines[0].Attrs["serviceId"] != "s1" {
		t.Errorf("serviceId = %q, want s1", lines[0].Attrs["serviceId"])
	}
	if lines[0].Attrs["nodeId"] != "n1" {
		t.Errorf("nodeId = %q, want n1", lines[0].Attrs["nodeId"])
	}
}

func TestParseDockerLogs_Empty(t *testing.T) {
	lines, err := ParseDockerLogs(bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("got %d lines, want 0", len(lines))
	}
}

func TestStreamDockerLogs(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z line1\n"))
	buf.Write(buildFrame(2, "2024-01-01T00:00:01.000000000Z line2\n"))

	ch := make(chan LogLine, 10)
	err := StreamDockerLogs(&buf, ch)
	close(ch)
	if err != nil {
		t.Fatal(err)
	}

	var lines []LogLine
	for l := range ch {
		lines = append(lines, l)
	}

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].Stream != "stdout" || lines[0].Message != "line1" {
		t.Errorf("lines[0] = %+v", lines[0])
	}
	if lines[1].Stream != "stderr" || lines[1].Message != "line2" {
		t.Errorf("lines[1] = %+v", lines[1])
	}
}

func TestParseDockerLogs_RejectsOversizedFrame(t *testing.T) {
	// Build a header with a size exceeding maxLogFrameSize.
	var buf bytes.Buffer
	buf.WriteByte(1)           // stdout
	buf.Write([]byte{0, 0, 0}) // padding
	_ = binary.Write(&buf, binary.BigEndian, uint32(maxLogFrameSize+1))
	// No need to write the full payload — the error should fire before reading it.

	_, err := ParseDockerLogs(&buf)
	if err == nil {
		t.Fatal("expected error for oversized frame, got nil")
	}
	if !strings.Contains(err.Error(), "log frame too large") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFilterSince(t *testing.T) {
	lines := []LogLine{
		{Timestamp: "2024-01-01T00:00:00.000000000Z", Message: "before"},
		{Timestamp: "2024-01-01T00:00:01.000000000Z", Message: "at cursor"},
		{Timestamp: "2024-01-01T00:00:02.000000000Z", Message: "after"},
	}

	t.Run("keeps only lines past the cursor", func(t *testing.T) {
		got := FilterSince(lines, "2024-01-01T00:00:01.000000000Z")

		if len(got) != 1 || got[0].Message != "after" {
			t.Errorf("FilterSince = %v, want just the line after the cursor", got)
		}
	})

	t.Run("returns everything when no cursor is given", func(t *testing.T) {
		if got := FilterSince(lines, ""); len(got) != len(lines) {
			t.Errorf(
				"FilterSince with empty cursor dropped lines: got %d, want %d",
				len(got),
				len(lines),
			)
		}
	})

	t.Run("keeps lines that carry no timestamp", func(t *testing.T) {
		untimed := []LogLine{{Message: "no timestamp"}}

		if got := FilterSince(untimed, "2024-01-01T00:00:01.000000000Z"); len(got) != 1 {
			t.Errorf("FilterSince dropped an untimestamped line")
		}
	})
}

// A multi-line log entry arrives as one frame whose continuation lines carry
// neither Docker's timestamp prefix nor its detail labels. Without inheriting
// them they cannot be ordered or cursored, so every reconnect re-sent every
// stack trace in the resume window, and a page whose surviving lines were all
// continuations produced no cursor at all.
func TestParseDockerLogs_ContinuationLinesInheritTheirParent(t *testing.T) {
	data := buildFrame(
		1,
		"2024-01-01T00:00:00.000000000Z com.docker.swarm.task.id=t1 panic: boom\n"+
			"\tgoroutine 1 [running]:\n"+
			"\tmain.main()\n",
	)

	lines, err := ParseDockerLogs(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	for _, line := range lines[1:] {
		if line.Timestamp != "2024-01-01T00:00:00.000000000Z" {
			t.Errorf(
				"continuation %q has timestamp %q, want its parent's",
				line.Message,
				line.Timestamp,
			)
		}
		if line.Attrs["taskId"] != "t1" {
			t.Errorf("continuation %q lost its task attribution: %v", line.Message, line.Attrs)
		}
	}
}

// Inheritance stops at the frame boundary, which is Docker's message
// boundary: the next frame may belong to another task entirely.
func TestParseDockerLogs_InheritanceStopsAtTheFrameBoundary(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(buildFrame(1, "2024-01-01T00:00:00.000000000Z first\n"))
	buf.Write(buildFrame(1, "stray line\n"))

	lines, err := ParseDockerLogs(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[1].Timestamp != "" {
		t.Errorf("timestamp = %q, want empty across a frame boundary", lines[1].Timestamp)
	}
}

func TestParseCursor(t *testing.T) {
	t.Run("normalises a non-UTC offset to the same instant", func(t *testing.T) {
		got, ok := ParseCursor("2024-01-01T12:00:00+02:00")
		if !ok {
			t.Fatal("ParseCursor rejected a valid RFC 3339 timestamp")
		}
		if got != "2024-01-01T10:00:00.000000000Z" {
			t.Errorf("ParseCursor = %q, want the same instant in UTC", got)
		}
	})

	t.Run("pads a second-precision timestamp", func(t *testing.T) {
		got, _ := ParseCursor("2024-01-01T10:00:00Z")
		if got != "2024-01-01T10:00:00.000000000Z" {
			t.Errorf("ParseCursor = %q, want nanosecond width", got)
		}
	})

	t.Run("resolves a Go duration to that long ago", func(t *testing.T) {
		got, ok := ParseCursor("30m")
		if !ok {
			t.Fatal("ParseCursor rejected a documented Go duration")
		}
		parsed, err := time.Parse(time.RFC3339Nano, got)
		if err != nil {
			t.Fatalf("ParseCursor produced an unparseable cursor %q: %v", got, err)
		}
		if delta := time.Since(parsed) - 30*time.Minute; delta.Abs() > time.Minute {
			t.Errorf("ParseCursor(30m) resolved to %v, want ~30m ago", parsed)
		}
	})

	t.Run("rejects an empty or unusable cursor", func(t *testing.T) {
		for _, cursor := range []string{"", "not-a-cursor"} {
			if _, ok := ParseCursor(cursor); ok {
				t.Errorf("ParseCursor(%q) accepted an unusable cursor", cursor)
			}
		}
	})
}

// A duration cursor used to be compared against Docker's timestamps as a raw
// string, where '2' < '3' drops every line ever logged.
func TestFilterSince_DurationCursor(t *testing.T) {
	now := time.Now().UTC()
	lines := []LogLine{
		{Timestamp: now.Add(-2 * time.Hour).Format(dockerTimeFormat), Message: "old"},
		{Timestamp: now.Add(-1 * time.Minute).Format(dockerTimeFormat), Message: "recent"},
	}

	got := FilterSince(lines, "30m")

	if len(got) != 1 || got[0].Message != "recent" {
		t.Errorf("FilterSince(30m) = %v, want just the line from the last 30 minutes", got)
	}
}

func TestBacklogFilter(t *testing.T) {
	const cursor = "2024-01-01T00:00:01.000000000Z"

	t.Run("drops the replayed backlog up to the cursor", func(t *testing.T) {
		filter := NewBacklogFilter(cursor)

		for _, line := range []LogLine{
			{Timestamp: "2024-01-01T00:00:00.000000000Z"},
			{Timestamp: cursor},
		} {
			if filter.Keep(line) {
				t.Errorf("kept %q, which the client already has", line.Timestamp)
			}
		}
	})

	// The cursor's whole purpose is de-duplicating the replay. Applying it to
	// live output as well means a node whose clock trails the one that
	// produced the cursor goes silent behind a green "Live" badge.
	t.Run("stops filtering a task once its backlog is past", func(t *testing.T) {
		filter := NewBacklogFilter(cursor)

		if !filter.Keep(LogLine{Timestamp: "2024-01-01T00:00:02.000000000Z"}) {
			t.Fatal("dropped a line past the cursor")
		}
		if !filter.Keep(LogLine{Timestamp: "2024-01-01T00:00:00.000000000Z"}) {
			t.Error("still filtering after the backlog ended")
		}
	})

	t.Run("tracks each task's backlog separately", func(t *testing.T) {
		filter := NewBacklogFilter(cursor)
		a := map[string]string{"taskId": "a"}
		b := map[string]string{"taskId": "b"}

		if !filter.Keep(LogLine{Timestamp: "2024-01-01T00:00:02.000000000Z", Attrs: a}) {
			t.Fatal("dropped a line past the cursor")
		}
		if filter.Keep(LogLine{Timestamp: "2024-01-01T00:00:00.000000000Z", Attrs: b}) {
			t.Error("another task's backlog was let through")
		}
	})

	t.Run("keeps every line when there is no cursor", func(t *testing.T) {
		filter := NewBacklogFilter("")

		if !filter.Keep(LogLine{Timestamp: "2024-01-01T00:00:00.000000000Z"}) {
			t.Error("a fresh stream dropped a line")
		}
	})
}
