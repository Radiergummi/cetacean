package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContentNegotiatedAtom(t *testing.T) {
	jsonH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("json"))
	})
	atomH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("atom"))
	})
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("spa"))
	})

	handler := contentNegotiated(jsonH, atomH, spa)

	t.Run("Atom content type dispatches to atom handler", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req = withContentType(req, ContentTypeAtom)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Body.String() != "atom" {
			t.Errorf("got %q, want %q", rec.Body.String(), "atom")
		}
	})

	t.Run("nil atom handler returns 406", func(t *testing.T) {
		handler := contentNegotiated(jsonH, nil, spa)
		req := httptest.NewRequest("GET", "/test", nil)
		req = withContentType(req, ContentTypeAtom)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusNotAcceptable {
			t.Errorf("got %d, want 406", rec.Code)
		}
	})
}
