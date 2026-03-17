package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

func TestRequireWrite_PassesThrough(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := requireWrite(inner)
	req := httptest.NewRequest("POST", "/services/abc/scale", nil)
	ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("inner handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestRequireWrite_NoIdentity_PassesThrough(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := requireWrite(inner)
	req := httptest.NewRequest("POST", "/services/abc/scale", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("inner handler was not called — requireWrite should be a pass-through today")
	}
}
