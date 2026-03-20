package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	json "github.com/goccy/go-json"
)

func TestRequireLevel_Allowed(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := requireLevel(1, 2)(inner)
	req := httptest.NewRequest("PUT", "/services/abc/scale", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("inner handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestRequireLevel_Denied(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := requireLevel(2, 1)(inner)
	req := httptest.NewRequest("PUT", "/nodes/abc/availability", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if called {
		t.Error("inner handler should not be called when level is insufficient")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403", w.Code)
	}

	var p ProblemDetail
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("failed to decode problem: %v", err)
	}
	if p.Status != 403 {
		t.Errorf("problem status=%d, want 403", p.Status)
	}
}

func TestRequireLevel_ReadOnly(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := requireLevel(1, 0)(inner)
	req := httptest.NewRequest("PUT", "/services/abc/scale", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if called {
		t.Error("inner handler should not be called in read-only mode")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403", w.Code)
	}
}

func TestRequireLevel_ExactMatch(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := requireLevel(2, 2)(inner)
	req := httptest.NewRequest("PUT", "/nodes/abc/availability", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("inner handler should be called when level exactly matches")
	}
}
