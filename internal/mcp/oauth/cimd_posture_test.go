package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fetchASMetadata returns the decoded RFC 8414 metadata document.
func fetchASMetadata(t *testing.T, s *Server) map[string]any {
	t.Helper()

	rec := httptest.NewRecorder()
	s.HandleMetadata(rec, httptest.NewRequest(
		http.MethodGet,
		"/.well-known/oauth-authorization-server",
		nil,
	))

	if rec.Code != http.StatusOK {
		t.Fatalf("metadata status = %d", rec.Code)
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}

	return doc
}

// TestMetadataAdvertisesCIMD — 2026-07-28 deprecates RFC 7591 DCR in favour of
// Client ID Metadata Documents. A client cannot know we accept an https://
// client_id unless we say so, and there is no other discovery path for it.
func TestMetadataAdvertisesCIMD(t *testing.T) {
	doc := fetchASMetadata(t, newTestServer(t))

	if supported, _ := doc["client_id_metadata_document_supported"].(bool); !supported {
		t.Error("AS metadata does not advertise client_id_metadata_document_supported")
	}

	// DCR stays advertised: deprecated is not removed, and clients that have
	// not migrated still need it.
	if doc["registration_endpoint"] == "" || doc["registration_endpoint"] == nil {
		t.Error("registration_endpoint dropped; DCR must stay available for older clients")
	}
}

// TestMetadataOmitsCIMDWhenDisabled — the advertisement has to track the
// switch, or an operator who turned CIMD off still tells clients to use it.
func TestMetadataOmitsCIMDWhenDisabled(t *testing.T) {
	s := newTestServer(t)
	s.cfg.MCP.CIMDEnabled = false

	if _, present := fetchASMetadata(t, s)["client_id_metadata_document_supported"]; present {
		t.Error("CIMD advertised while disabled")
	}
}

// TestCIMDDisabledRejectsHTTPSClientID is the switch actually doing something.
// CIMD makes the server fetch a URL the client supplies, so an operator who
// disables it is removing outbound request surface; if the code still fetches,
// the setting is a false assurance.
func TestCIMDDisabledRejectsHTTPSClientID(t *testing.T) {
	s := newTestServer(t)
	s.cfg.MCP.CIMDEnabled = false

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)

	meta, verified, errMsg := s.resolveClientMeta(req, "https://client.example/metadata.json")
	if meta != nil || verified {
		t.Fatal("CIMD client resolved while CIMD is disabled")
	}

	if errMsg == "" {
		t.Error("expected an explanatory error message")
	}
}

// TestCIMDEnabledStillResolves guards the default path.
func TestCIMDEnabledStillResolves(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)

	// An unreachable URL still proves the CIMD branch was taken: a disabled
	// server refuses before fetching, with a different message.
	_, _, errMsg := s.resolveClientMeta(req, "https://client.invalid/metadata.json")
	if errMsg == "" {
		t.Fatal("expected a fetch failure message")
	}

	if errMsg == cimdDisabledMessage {
		t.Fatal("CIMD refused while enabled")
	}
}
