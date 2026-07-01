package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
