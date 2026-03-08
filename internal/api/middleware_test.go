package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"cetacean/internal/cache"
)

func TestRequestLogger(t *testing.T) {
	handler := requestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest("GET", "/api/nodes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status=%d, want 200", w.Code)
	}
	if w.Body.String() != "hello" {
		t.Errorf("body=%s, want hello", w.Body.String())
	}
}

func TestRecovery(t *testing.T) {
	handler := recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Errorf("status=%d, want 500", w.Code)
	}
}

func TestStatusWriter_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: 200}

	sw.Flush()
	if !rec.Flushed {
		t.Error("Flush() should delegate to underlying writer")
	}
}

func TestStatusWriter_Unwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: 200}

	if sw.Unwrap() != rec {
		t.Error("Unwrap() should return underlying ResponseWriter")
	}
}

func TestRequestLogger_SkipsAssets(t *testing.T) {
	called := false
	handler := requestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/assets/main.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("handler should still be called")
	}
	if w.Code != 200 {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestRequestLogger_4xxLevel(t *testing.T) {
	handler := requestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest("GET", "/api/missing", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestRequestLogger_5xxLevel(t *testing.T) {
	handler := requestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest("GET", "/api/error", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Errorf("status=%d, want 500", w.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options=%q, want nosniff", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options=%q, want DENY", got)
	}
}

func TestNewSPAHandler_ServesFile(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":     {Data: []byte("<html>app</html>")},
		"assets/main.js": {Data: []byte("console.log('ok')")},
	}
	handler := NewSPAHandler(fs.FS(fsys))

	req := httptest.NewRequest("GET", "/assets/main.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	if w.Body.String() != "console.log('ok')" {
		t.Errorf("body=%q", w.Body.String())
	}
}

func TestNewSPAHandler_FallbackToIndex(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {Data: []byte("<html>app</html>")},
	}
	handler := NewSPAHandler(fs.FS(fsys))

	req := httptest.NewRequest("GET", "/some/route", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

func TestNewRouter_Smoke(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, closedReady(), nil)
	b := NewBroadcaster()
	defer b.Close()
	prom := NewPrometheusProxy("http://localhost:9090")
	fsys := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	spa := NewSPAHandler(fs.FS(fsys))

	router := NewRouter(h, b, prom, spa)
	if router == nil {
		t.Fatal("NewRouter returned nil")
	}

	// Verify a known route works
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	// Verify security headers from middleware chain
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options=%q, want nosniff", got)
	}
}
