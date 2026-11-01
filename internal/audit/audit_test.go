package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestStreamTagsEveryLine(t *testing.T) {
	var buf bytes.Buffer
	s := NewStream(&buf)

	if err := s.Write(
		Record{Time: time.Unix(0, 0).UTC(), Event: "exec.start", Session: "s1"},
	); err != nil {
		t.Fatalf("write: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not one JSON line: %q", buf.String())
	}
	if got["log"] != "audit" || got["event"] != "exec.start" || got["session"] != "s1" {
		t.Errorf("record = %v", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestStreamIsBestEffort(t *testing.T) {
	if err := NewStream(failingWriter{}).Write(Record{Event: "exec.start"}); err != nil {
		t.Errorf("stdout output failed a write: %v", err)
	}
}
