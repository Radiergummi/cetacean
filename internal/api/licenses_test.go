package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleLicensesServesProjectedJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/-/licenses", nil)
	rec := httptest.NewRecorder()

	HandleLicenses(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var body struct {
		Components []map[string]any `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if len(body.Components) == 0 {
		t.Error("expected components in projected licenses response")
	}
}

func TestHandleSBOMServesCycloneDXWithETag304(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/-/sbom.cdx.json", nil)
	rec := httptest.NewRecorder()

	HandleSBOM(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/vnd.cyclonedx+json") {
		t.Errorf("content-type = %q, want prefix application/vnd.cyclonedx+json", ct)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header")
	}

	// Conditional request with the same ETag must yield 304.
	req2 := httptest.NewRequest(http.MethodGet, "/-/sbom.cdx.json", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()

	HandleSBOM(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", rec2.Code)
	}
}

// TestLicensesEndpointsRouteThroughNegotiate is the regression guard for the
// routing deviation described in skipEndpoint: the negotiate middleware strips
// the .json suffix from /-/sbom.cdx.json before mux dispatch, so the mux
// route is registered as /-/sbom.cdx. This test exercises the full
// negotiate-wrapped mux to confirm both public meta endpoints are reachable at
// their canonical URLs without the full production router.
func TestLicensesEndpointsRouteThroughNegotiate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /-/licenses", HandleLicenses)
	mux.HandleFunc("GET /-/sbom.cdx", HandleSBOM)
	handler := negotiate(mux)

	t.Run("SBOM canonical URL strips .json and routes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/-/sbom.cdx.json", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/vnd.cyclonedx+json") {
			t.Errorf("content-type = %q, want prefix application/vnd.cyclonedx+json", ct)
		}
	})

	t.Run("licenses endpoint routes directly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/-/licenses", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
	})
}
