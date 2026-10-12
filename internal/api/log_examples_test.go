package api

import (
	"bytes"
	"encoding/binary"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/radiergummi/cetacean/internal/logs"
)

func TestLogLineExamplesCarryParsedAttrs(t *testing.T) {
	payload := "2026-04-13T08:12:34.123456789Z com.docker.swarm.node.id=n1," +
		"com.docker.swarm.service.id=s1,com.docker.swarm.task.id=t1 hello\n"

	var frame bytes.Buffer
	frame.Write([]byte{1, 0, 0, 0})
	_ = binary.Write(&frame, binary.BigEndian, uint32(len(payload)))
	frame.WriteString(payload)

	lines, err := logs.ParseDockerLogs(&frame)
	if err != nil || len(lines) != 1 {
		t.Fatalf("parse: %v, %d lines", err, len(lines))
	}

	want := slices.Sorted(maps.Keys(lines[0].Attrs))

	openAPI, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}

	docs := map[string]map[string]any{
		"openapi.yaml":  loadYAMLDocument(t, "openapi.yaml", openAPI),
		"asyncapi.yaml": loadAsyncAPIDoc(t),
	}

	for name, doc := range docs {
		var found int

		walkLogLineExamples(doc, func(attrs map[string]any) {
			found++

			if got := slices.Sorted(maps.Keys(attrs)); !slices.Equal(got, want) {
				t.Errorf("%s: log line example attrs %v, parser emits %v", name, got, want)
			}
		})

		if found == 0 {
			t.Errorf("%s: no log line example found", name)
		}
	}
}

// walkLogLineExamples calls fn with the attrs of every value shaped like a
// log line: a message and a stream string beside an attrs object.
func walkLogLineExamples(node any, fn func(map[string]any)) {
	switch v := node.(type) {
	case map[string]any:
		_, hasMessage := v["message"].(string)
		_, hasStream := v["stream"].(string)
		if attrs, ok := v["attrs"].(map[string]any); ok && hasMessage && hasStream {
			fn(attrs)
		}

		for _, child := range v {
			walkLogLineExamples(child, fn)
		}
	case []any:
		for _, child := range v {
			walkLogLineExamples(child, fn)
		}
	}
}
