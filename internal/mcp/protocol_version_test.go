package mcp

import (
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// TestNegotiatesLatestProtocol pins the protocol revision Cetacean advertises.
// It fails loudly on a library bump that changes the negotiated version, which
// is exactly when the rest of this package needs re-reading.
func TestNegotiatesLatestProtocol(t *testing.T) {
	if got, want := mcplib.LATEST_PROTOCOL_VERSION, "2026-07-28"; got != want {
		t.Fatalf("LATEST_PROTOCOL_VERSION = %q, want %q", got, want)
	}

	// Legacy clients must still be able to complete an initialize handshake.
	if got, want := mcplib.LATEST_LEGACY_PROTOCOL_VERSION, "2025-11-25"; got != want {
		t.Fatalf("LATEST_LEGACY_PROTOCOL_VERSION = %q, want %q", got, want)
	}

	for _, v := range []string{"2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"} {
		if !mcplib.IsValidProtocolVersion(v) {
			t.Errorf("protocol version %q no longer supported", v)
		}
	}

	if !mcplib.IsModernProtocol("2026-07-28") {
		t.Error("2026-07-28 should be classified as modern (stateless core)")
	}

	if mcplib.IsModernProtocol("2025-11-25") {
		t.Error("2025-11-25 should be classified as legacy (initialize handshake)")
	}
}

// TestResourceNotFoundCodeIsVersionAware documents that -32002 -> -32602 (issue
// #73) is handled by mcp-go per negotiated version, not by us. A legacy session
// must still see -32002.
func TestResourceNotFoundCodeIsVersionAware(t *testing.T) {
	if mcplib.RESOURCE_NOT_FOUND == mcplib.INVALID_PARAMS {
		t.Fatal("expected distinct codes for the two eras")
	}

	if got, want := mcplib.INVALID_PARAMS, -32602; got != want {
		t.Fatalf("INVALID_PARAMS = %d, want %d", got, want)
	}

	if got, want := mcplib.RESOURCE_NOT_FOUND, -32002; got != want {
		t.Fatalf("RESOURCE_NOT_FOUND = %d, want %d", got, want)
	}
}
