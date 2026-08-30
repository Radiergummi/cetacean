package mcp

import (
	"encoding/json"
	"testing"
)

func TestServerExtensionsDeclareUIMIMEType(t *testing.T) {
	ext := serverExtensions()

	ui, ok := ext[extensionUI].(map[string]any)
	if !ok {
		t.Fatalf("no %q extension declared: hosts will not render widgets", extensionUI)
	}

	mimeTypes, ok := ui["mimeTypes"].([]string)
	if !ok || len(mimeTypes) == 0 {
		t.Fatalf("ui extension declares no mimeTypes, got %#v", ui["mimeTypes"])
	}

	if mimeTypes[0] != uiMIMEType {
		t.Fatalf("mimeTypes[0] = %q, want %q", mimeTypes[0], uiMIMEType)
	}
}

func TestServerExtensionsDeclareTasks(t *testing.T) {
	if _, ok := serverExtensions()[extensionTasks]; !ok {
		t.Fatalf("no %q extension declared", extensionTasks)
	}
}

// TestDiscoverAdvertisesExtensions drives server/discover, the 2026-07-28
// replacement for the initialize handshake. A host learns what we support from
// this response alone, so an unwired capability is invisible to it.
func TestDiscoverAdvertisesExtensions(t *testing.T) {
	handler := newTestServer(t).Handler()

	_, env := mcpModern(t, handler, 1, "server/discover", `{}`)
	if env.Error != nil {
		t.Fatalf("server/discover error: %+v", env.Error)
	}

	var result struct {
		Capabilities struct {
			Extensions map[string]json.RawMessage `json:"extensions"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode server/discover: %v", err)
	}

	for _, name := range []string{extensionTasks, extensionUI} {
		if _, ok := result.Capabilities.Extensions[name]; !ok {
			t.Errorf("server/discover does not advertise %q (got %v)",
				name, result.Capabilities.Extensions)
		}
	}
}
