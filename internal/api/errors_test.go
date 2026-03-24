package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

	var defs []ErrorDef
	if err := json.Unmarshal(w.Body.Bytes(), &defs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(defs) != len(errorRegistry) {
		t.Errorf("got %d errors, want %d", len(defs), len(errorRegistry))
	}
}

func TestHandleErrorIndex_HTML(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/errors", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()

	HandleErrorIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type=%s, want text/html", ct)
	}
	if !strings.Contains(w.Body.String(), "NOD001") {
		t.Error("HTML response does not contain NOD001")
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

func TestHandleErrorDetail_HTML(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/errors/VOL001", nil)
	req.SetPathValue("code", "VOL001")
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()

	HandleErrorDetail(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "VOL001") {
		t.Error("HTML does not contain error code")
	}
	if !strings.Contains(body, "Volume In Use") {
		t.Error("HTML does not contain error title")
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
