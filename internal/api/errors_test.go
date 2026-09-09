package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	json "github.com/goccy/go-json"
)

func TestHandleErrorIndex_JSON(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/errors", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	HandleErrorIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type=%s, want application/json", ct)
	}

	var resp CollectionResponse[ErrorDef]
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Items) != len(errorRegistry) {
		t.Errorf("got %d errors, want %d", len(resp.Items), len(errorRegistry))
	}
}

func TestHandleErrorDetail_JSON(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/errors/NOD001", nil)
	req.SetPathValue("code", "NOD001")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	HandleErrorDetail(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var def ErrorDef
	if err := json.Unmarshal(w.Body.Bytes(), &def); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if def.Code != "NOD001" {
		t.Errorf("code=%s, want NOD001", def.Code)
	}
	if def.Status != http.StatusConflict {
		t.Errorf("status=%d, want 409", def.Status)
	}
}

func TestHandleErrorDetail_NotFound(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/errors/XXX999", nil)
	req.SetPathValue("code", "XXX999")
	w := httptest.NewRecorder()

	HandleErrorDetail(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestWriteErrorCode_KnownCode(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/nodes/node1", nil)
	w := httptest.NewRecorder()

	writeErrorCode(w, req, "NOD001", "node xyz is not down and can't be removed")

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["type"] != "/api/errors/NOD001" {
		t.Errorf("type=%v, want /api/errors/NOD001", body["type"])
	}
	if body["title"] != "Node Not Down" {
		t.Errorf("title=%v, want Node Not Down", body["title"])
	}
	if body["detail"] != "node xyz is not down and can't be removed" {
		t.Errorf("detail=%v", body["detail"])
	}
}

func TestWriteErrorCode_UnknownCode(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/test", nil)
	w := httptest.NewRecorder()

	writeErrorCode(w, req, "XXX999", "something went wrong")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}
}

func TestErrorDomainsMatchTheRegistry(t *testing.T) {
	labelled := make(map[string]bool, len(ErrorDomains))

	for _, domain := range ErrorDomains {
		if labelled[domain.Prefix] {
			t.Errorf("ErrorDomains lists %q twice", domain.Prefix)
		}
		if domain.Label == "" {
			t.Errorf("domain %q has no label", domain.Prefix)
		}

		labelled[domain.Prefix] = true
	}

	used := make(map[string]bool, len(ErrorDomains))

	for code := range errorRegistry {
		if len(code) != 6 {
			t.Errorf("code %q is not three letters and three digits", code)
			continue
		}

		prefix := code[:3]
		used[prefix] = true

		if !labelled[prefix] {
			t.Errorf("code %s uses prefix %q, which ErrorDomains does not name", code, prefix)
		}
	}

	for _, domain := range ErrorDomains {
		if !used[domain.Prefix] {
			t.Errorf("ErrorDomains names %q, which no error code uses", domain.Prefix)
		}
	}
}

func TestErrorDefsAreCompleteAndSorted(t *testing.T) {
	defs := ErrorDefs()

	if len(defs) != len(errorRegistry) {
		t.Fatalf("ErrorDefs returned %d entries, want %d", len(defs), len(errorRegistry))
	}

	for i, def := range defs {
		if def.Code == "" || def.Title == "" || def.Description == "" || def.Suggestion == "" {
			t.Errorf("entry %d (%q) has an empty field: %+v", i, def.Code, def)
		}
		if def.Status < 400 || def.Status > 599 {
			t.Errorf("%s has status %d, want a 4xx or 5xx", def.Code, def.Status)
		}
		if i > 0 && defs[i-1].Code >= def.Code {
			t.Errorf("ErrorDefs is not sorted: %q precedes %q", defs[i-1].Code, def.Code)
		}
	}
}
