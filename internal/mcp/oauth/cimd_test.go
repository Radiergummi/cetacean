package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// serveMetadata returns an http.HandlerFunc that serves a ClientMetadata JSON
// document with client_id set to clientID. If clientID is empty, the handler
// sets it to the host from the incoming request (i.e. the test server URL).
func serveMetadata(clientID string, meta ClientMetadata) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := meta
		if clientID != "" {
			m.ClientID = clientID
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(m); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	}
}

// TestCIMDFetchValid verifies that a well-formed document is fetched and parsed.
func TestCIMDFetchValid(t *testing.T) {
	var serverURL string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := ClientMetadata{
			ClientID:     serverURL + r.URL.Path,
			ClientName:   "Test Client",
			RedirectURIs: []string{"https://example.com/cb"},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(meta); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL

	fetcher := &CIMDFetcher{
		Client:        srv.Client(),
		AllowLoopback: true,
	}

	clientID := srv.URL + "/client"
	meta, err := fetcher.Fetch(t.Context(), clientID)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if meta.ClientID != clientID {
		t.Errorf("ClientID = %q, want %q", meta.ClientID, clientID)
	}
	if meta.ClientName != "Test Client" {
		t.Errorf("ClientName = %q, want %q", meta.ClientName, "Test Client")
	}
}

// TestCIMDFetchRejectsHTTP ensures plain HTTP client_ids are rejected before
// any network IO.
func TestCIMDFetchRejectsHTTP(t *testing.T) {
	fetcher := &CIMDFetcher{}
	_, err := fetcher.Fetch(t.Context(), "http://example.com/client")
	if !errors.Is(err, ErrCIMDInvalidURL) {
		t.Fatalf("expected ErrCIMDInvalidURL, got: %v", err)
	}
}

// TestCIMDFetchRejectsLoopbackByDefault ensures loopback addresses are blocked
// when AllowLoopback is false.
func TestCIMDFetchRejectsLoopbackByDefault(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fetcher := &CIMDFetcher{
		Client:        srv.Client(),
		AllowLoopback: false,
	}

	_, err := fetcher.Fetch(t.Context(), srv.URL+"/client")
	if !errors.Is(err, ErrCIMDSSRFBlocked) {
		t.Fatalf("expected ErrCIMDSSRFBlocked, got: %v", err)
	}
}

// TestCIMDFetchClientIDMismatch ensures the client_id in the document must
// match the requested URL exactly.
func TestCIMDFetchClientIDMismatch(t *testing.T) {
	srv := httptest.NewTLSServer(serveMetadata("https://wrong.example.com/client", ClientMetadata{
		ClientName: "Wrong",
	}))
	defer srv.Close()

	fetcher := &CIMDFetcher{
		Client:        srv.Client(),
		AllowLoopback: true,
	}

	_, err := fetcher.Fetch(t.Context(), srv.URL+"/client")
	if !errors.Is(err, ErrCIMDClientIDMismatch) {
		t.Fatalf("expected ErrCIMDClientIDMismatch, got: %v", err)
	}
}

// TestCIMDFetchResponseTooLarge ensures responses over 5 KiB are rejected.
func TestCIMDFetchResponseTooLarge(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Write more than 5 KiB of data (not valid JSON, but we check size first)
		w.Write([]byte(`{"client_id":"` + strings.Repeat("x", 6*1024) + `"}`))
	}))
	defer srv.Close()

	fetcher := &CIMDFetcher{
		Client:        srv.Client(),
		AllowLoopback: true,
	}

	_, err := fetcher.Fetch(t.Context(), srv.URL+"/client")
	if !errors.Is(err, ErrCIMDOversize) {
		t.Fatalf("expected ErrCIMDOversize, got: %v", err)
	}
}

// TestCIMDFetchSymmetricAuthRejected ensures symmetric auth methods are refused.
func TestCIMDFetchSymmetricAuthRejected(t *testing.T) {
	for _, method := range []string{"client_secret_post", "client_secret_basic"} {
		method := method
		t.Run(method, func(t *testing.T) {
			var serverURL string
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				meta := ClientMetadata{
					ClientID:                serverURL + r.URL.Path,
					TokenEndpointAuthMethod: method,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(meta)
			}))
			defer srv.Close()
			serverURL = srv.URL

			fetcher := &CIMDFetcher{
				Client:        srv.Client(),
				AllowLoopback: true,
			}

			_, err := fetcher.Fetch(t.Context(), srv.URL+"/client")
			if !errors.Is(err, ErrCIMDSymmetricAuth) {
				t.Fatalf("expected ErrCIMDSymmetricAuth for %q, got: %v", method, err)
			}
		})
	}
}

// TestCIMDFetchRejectsFragmentOrCredentials ensures fragment and userinfo are
// rejected before any network IO.
func TestCIMDFetchRejectsFragmentOrCredentials(t *testing.T) {
	cases := []struct {
		name     string
		clientID string
	}{
		{"fragment", "https://example.com/x#frag"},
		{"userinfo", "https://user:pass@example.com/x"},
	}
	fetcher := &CIMDFetcher{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fetcher.Fetch(t.Context(), tc.clientID)
			if !errors.Is(err, ErrCIMDInvalidURL) {
				t.Fatalf("expected ErrCIMDInvalidURL for %q, got: %v", tc.clientID, err)
			}
		})
	}
}

// TestCIMDFetchCachesResults ensures the second call for the same URL does not
// hit the server.
func TestCIMDFetchCachesResults(t *testing.T) {
	var callCount atomic.Int32
	var serverURL string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		meta := ClientMetadata{
			ClientID:   serverURL + r.URL.Path,
			ClientName: "Cached Client",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	}))
	defer srv.Close()
	serverURL = srv.URL

	fetcher := &CIMDFetcher{
		Client:        srv.Client(),
		AllowLoopback: true,
	}

	clientID := srv.URL + "/client"
	if _, err := fetcher.Fetch(t.Context(), clientID); err != nil {
		t.Fatalf("first fetch failed: %v", err)
	}
	if _, err := fetcher.Fetch(t.Context(), clientID); err != nil {
		t.Fatalf("second fetch failed: %v", err)
	}

	if n := callCount.Load(); n != 1 {
		t.Errorf("server was called %d times, want 1 (cache miss on second call)", n)
	}
}

// TestCIMDFetchHasRedirectURI verifies the exact-match helper.
func TestCIMDFetchHasRedirectURI(t *testing.T) {
	meta := &ClientMetadata{
		ClientID:     "https://example.com/client",
		RedirectURIs: []string{"https://example.com/cb", "https://example.com/cb2"},
	}

	if !meta.HasRedirectURI("https://example.com/cb") {
		t.Error("expected HasRedirectURI to return true for registered URI")
	}
	if meta.HasRedirectURI("https://example.com/OTHER") {
		t.Error("expected HasRedirectURI to return false for unregistered URI")
	}
	if meta.HasRedirectURI("") {
		t.Error("expected HasRedirectURI to return false for empty string")
	}
}

// TestCIMDFetchRejectsEmptyPath ensures that a URL without a meaningful path
// component is rejected.
func TestCIMDFetchRejectsEmptyPath(t *testing.T) {
	for _, clientID := range []string{
		"https://example.com",
		"https://example.com/",
	} {
		_, err := (&CIMDFetcher{}).Fetch(t.Context(), clientID)
		if !errors.Is(err, ErrCIMDInvalidURL) {
			t.Errorf("expected ErrCIMDInvalidURL for %q, got: %v", clientID, err)
		}
	}
}

// TestCIMDFetchNonOKStatus verifies that non-200 responses are rejected.
func TestCIMDFetchNonOKStatus(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	fetcher := &CIMDFetcher{
		Client:        srv.Client(),
		AllowLoopback: true,
	}

	_, err := fetcher.Fetch(t.Context(), srv.URL+"/client")
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
	if fmt.Sprintf("%v", err) == "" {
		t.Fatal("error message is empty")
	}
}
