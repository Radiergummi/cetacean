package mcp

import (
	"encoding/json"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestStructuredToolResultCarriesParsedObject(t *testing.T) {
	res := structuredToolResult(`{"removed":true}`)

	if res.StructuredContent == nil {
		t.Fatal("expected StructuredContent to be set")
	}

	// structuredContent is carried as raw JSON bytes (no lossy map round-trip);
	// it must serialize to the original object.
	raw, ok := res.StructuredContent.(json.RawMessage)
	if !ok {
		t.Fatalf("StructuredContent type = %T, want json.RawMessage", res.StructuredContent)
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("structuredContent is not valid JSON: %v", err)
	}
	if obj["removed"] != true {
		t.Errorf("removed = %v, want true", obj["removed"])
	}

	if len(res.Content) == 0 {
		t.Fatal("expected a text content fallback for pre-2025-06-18 clients")
	}
	text, ok := res.Content[0].(mcplib.TextContent)
	if !ok {
		t.Fatalf("content[0] type = %T, want TextContent", res.Content[0])
	}
	if text.Text != `{"removed":true}` {
		t.Errorf("fallback text = %q, want the raw JSON", text.Text)
	}
}

func TestStructuredToolResultDegradesForNonObject(t *testing.T) {
	// Tool output is always a JSON object today, but a top-level array or scalar
	// must not be forced into structuredContent (the spec requires an object).
	res := structuredToolResult(`["a","b"]`)

	if res.StructuredContent != nil {
		t.Errorf(
			"expected text-only result for a non-object, got StructuredContent %v",
			res.StructuredContent,
		)
	}
	if len(res.Content) == 0 {
		t.Fatal("expected the raw text to be preserved")
	}
}
