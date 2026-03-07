package api

import (
	"bytes"
	"encoding/binary"
	"testing"
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
	data := buildFrame(1, "2024-01-01T00:00:00.000000000Z line1\n2024-01-01T00:00:01.000000000Z line2\n")
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
