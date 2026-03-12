package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteProblem(t *testing.T) {
	req := httptest.NewRequest("GET", "/nodes/missing", nil)
	ctx := context.WithValue(req.Context(), reqIDKey, "test-req-123")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	writeProblem(w, req, http.StatusNotFound, "node \"missing\" not found")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}

	var p ProblemDetail
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Context != "/api/context.jsonld" {
		t.Errorf("@context = %q", p.Context)
	}
	if p.Type != "about:blank" {
		t.Errorf("type = %q", p.Type)
	}
	if p.Title != "Not Found" {
		t.Errorf("title = %q", p.Title)
	}
	if p.Status != 404 {
		t.Errorf("status = %d", p.Status)
	}
	if p.Detail != `node "missing" not found` {
		t.Errorf("detail = %q", p.Detail)
	}
	if p.Instance != "/nodes/missing" {
		t.Errorf("instance = %q", p.Instance)
	}
	if p.RequestID != "test-req-123" {
		t.Errorf("requestId = %q", p.RequestID)
	}
}

func TestWriteProblem_NoRequestID(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	writeProblem(w, req, http.StatusBadRequest, "bad input")

	var p ProblemDetail
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.RequestID != "" {
		t.Errorf("requestId = %q, want empty", p.RequestID)
	}
}

func TestWriteProblemTyped(t *testing.T) {
	req := httptest.NewRequest("GET", "/nodes?filter=bad", nil)
	ctx := context.WithValue(req.Context(), reqIDKey, "typed-req-456")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	writeProblemTyped(w, req, ProblemDetail{
		Type:   "urn:cetacean:error:filter-invalid",
		Title:  "Invalid Filter Expression",
		Status: http.StatusBadRequest,
		Detail: "syntax error at position 3",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var p ProblemDetail
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Type != "urn:cetacean:error:filter-invalid" {
		t.Errorf("type = %q", p.Type)
	}
	if p.Title != "Invalid Filter Expression" {
		t.Errorf("title = %q", p.Title)
	}
	if p.Context != "/api/context.jsonld" {
		t.Errorf("@context = %q", p.Context)
	}
	if p.Instance != "/nodes" {
		t.Errorf("instance = %q", p.Instance)
	}
	if p.RequestID != "typed-req-456" {
		t.Errorf("requestId = %q", p.RequestID)
	}
}

func TestWriteProblemTyped_DefaultsFilled(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	writeProblemTyped(w, req, ProblemDetail{
		Status: http.StatusInternalServerError,
	})

	var p ProblemDetail
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Type != "about:blank" {
		t.Errorf("type = %q, want about:blank", p.Type)
	}
	if p.Title != "Internal Server Error" {
		t.Errorf("title = %q", p.Title)
	}
}
