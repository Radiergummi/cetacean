package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/spec"
)

// serveMetadata returns an http.HandlerFunc that serves a ClientMetadata JSON
// document. When clientID is non-empty it overrides meta.ClientID; otherwise
// the document is returned as-is.
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
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/client-id-url-uses-https",
	)

	fetcher := &CIMDFetcher{}
	_, err := fetcher.Fetch(t.Context(), "http://example.com/client")
	if !errors.Is(err, ErrCIMDInvalidURL) {
		t.Fatalf("expected ErrCIMDInvalidURL, got: %v", err)
	}
}

// TestCIMDFetchRejectsLoopbackByDefault ensures loopback addresses are blocked
// when AllowLoopback is false.
func TestCIMDFetchRejectsLoopbackByDefault(t *testing.T) {
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/special-use-addresses-not-fetched",
		"oauth/draft-ietf-oauth-client-id-metadata-document/loopback-exception-not-in-production",
	)

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
	spec.Satisfies(
		t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/document-client-id-matches-the-fetched-url",
	)

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
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/response-size-limited",
	)

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
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/no-symmetric-token-endpoint-auth",
	)

	for _, method := range []string{
		"client_secret_post",
		"client_secret_basic",
		"client_secret_jwt",
	} {
		t.Run(method, func(t *testing.T) {
			var serverURL string
			srv := httptest.NewTLSServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					meta := ClientMetadata{
						ClientID:                serverURL + r.URL.Path,
						TokenEndpointAuthMethod: method,
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(meta)
				}),
			)
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
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/client-id-has-no-userinfo",
		"oauth/draft-ietf-oauth-client-id-metadata-document/client-id-has-no-fragment",
	)

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
	spec.Satisfies(
		t,
		"oauth/rfc8252/redirect-uri-registered-and-exact-matched",
		"oauth/draft-ietf-oauth-client-id-metadata-document/redirect-urls-registered-and-exact-matched",
	)

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
	// A URI path is case-sensitive, so folding case here would hand the code
	// to a target the client never registered.
	if meta.HasRedirectURI("https://example.com/CB") {
		t.Error("expected HasRedirectURI to return false for a case-folded path")
	}
	if meta.HasRedirectURI("") {
		t.Error("expected HasRedirectURI to return false for empty string")
	}
}

// TestCIMDFetchRejectsEmptyPath ensures that a URL without a meaningful path
// component is rejected.
func TestCIMDFetchRejectsEmptyPath(t *testing.T) {
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/client-id-has-a-path",
	)

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
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/only-200-is-a-document",
	)

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

func TestCIMDFetchBlocksCGNAT(t *testing.T) {
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/special-use-addresses-not-fetched",
	)

	f := &CIMDFetcher{}
	if err := f.checkIP(net.ParseIP("100.64.1.1")); !errors.Is(err, ErrCIMDSSRFBlocked) {
		t.Errorf("100.64.1.1: got %v, want errors.Is(ErrCIMDSSRFBlocked)", err)
	}
	if err := f.checkIP(net.ParseIP("100.127.255.254")); !errors.Is(err, ErrCIMDSSRFBlocked) {
		t.Errorf("100.127.255.254 (upper CGNAT): got %v, want errors.Is(ErrCIMDSSRFBlocked)", err)
	}
	// Just outside CGNAT range should not trip CGNAT (other checks might).
	if err := f.checkIP(net.ParseIP("100.63.255.255")); errors.Is(err, ErrCIMDSSRFBlocked) {
		// Allowed unless caught by another rule; we only care that the CGNAT branch did not falsely match.
		if !strings.Contains(err.Error(), "CGNAT") {
			// Other SSRF reason is OK.
		} else {
			t.Errorf("100.63.255.255: incorrectly classified as CGNAT")
		}
	}
}

// The cache is bounded. client_id is the caller's to choose and the document is
// served by a host they control, so without a cap an authorize loop over
// distinct URLs would grow this map for as long as it ran.
func TestCIMDCacheIsBounded(t *testing.T) {
	f := &CIMDFetcher{}

	for i := range cimdCacheMaxEntries * 2 {
		f.cachePut(fmt.Sprintf("https://client.example/%d", i), &ClientMetadata{})
	}

	if got := len(f.cache); got > cimdCacheMaxEntries {
		t.Errorf("cache holds %d entries, want at most %d", got, cimdCacheMaxEntries)
	}
}

// Eviction spends the lapsed entries first: dropping one costs nothing, while
// dropping a live one costs its client a re-fetch.
func TestCIMDCacheEvictsLapsedEntriesFirst(t *testing.T) {
	f := &CIMDFetcher{cache: make(map[string]cachedEntry)}

	stale := time.Now().Add(-2 * cimdCacheTTL)
	for i := range cimdCacheMaxEntries {
		f.cache[fmt.Sprintf("https://lapsed.example/%d", i)] = cachedEntry{
			meta:      &ClientMetadata{},
			fetchedAt: stale,
		}
	}

	const live = "https://live.example/id"
	f.cachePut(live, &ClientMetadata{})

	if f.cacheGet(live) == nil {
		t.Fatal("the entry that triggered eviction was not stored")
	}
	if got := len(f.cache); got != 1 {
		t.Errorf("cache holds %d entries, want only the live one", got)
	}
}

// Re-fetching a client already cached must not spend a slot, or a single client
// refreshing in a loop would evict every other one.
func TestCIMDCacheReplacesWithoutEvicting(t *testing.T) {
	f := &CIMDFetcher{cache: make(map[string]cachedEntry)}

	for i := range cimdCacheMaxEntries {
		f.cache[fmt.Sprintf("https://client.example/%d", i)] = cachedEntry{
			meta:      &ClientMetadata{},
			fetchedAt: time.Now(),
		}
	}

	f.cachePut("https://client.example/0", &ClientMetadata{ClientName: "renamed"})

	if got := len(f.cache); got != cimdCacheMaxEntries {
		t.Errorf("cache holds %d entries, want %d", got, cimdCacheMaxEntries)
	}
	if meta := f.cacheGet("https://client.example/0"); meta == nil || meta.ClientName != "renamed" {
		t.Error("the replacement did not land")
	}
}

// fetchServed fetches one document from a handler that is given the client_id
// it is served under, so each case writes only what it varies.
func fetchServed(
	t *testing.T,
	handler func(clientID string, w http.ResponseWriter, r *http.Request),
) error {
	t.Helper()

	var serverURL string

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(serverURL+"/client", w, r)
	}))
	t.Cleanup(srv.Close)
	serverURL = srv.URL

	fetcher := &CIMDFetcher{Client: srv.Client(), AllowLoopback: true}
	_, err := fetcher.Fetch(t.Context(), srv.URL+"/client")

	return err
}

func validDocument(clientID string) ClientMetadata {
	return ClientMetadata{
		ClientID:     clientID,
		ClientName:   "Test Client",
		RedirectURIs: []string{"https://example.com/cb"},
	}
}

// 169.254.169.254 is the metadata service on every major cloud: link-local,
// and neither private nor loopback, so only its own check refuses it.
func TestCIMDRefusesLinkLocalAddresses(t *testing.T) {
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/special-use-addresses-not-fetched",
	)

	f := &CIMDFetcher{}

	for _, addr := range []string{"169.254.169.254", "fe80::1"} {
		if err := f.checkIP(net.ParseIP(addr)); !errors.Is(err, ErrCIMDSSRFBlocked) {
			t.Errorf("checkIP(%s) = %v, want ErrCIMDSSRFBlocked", addr, err)
		}
	}
}

// Every other test dials an IP literal, but a client_id names a host, and the
// address it resolves to is what a rebinding attacker controls.
func TestCIMDDialScreensTheAddressesANameResolvesTo(t *testing.T) {
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/special-use-addresses-not-fetched",
	)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	address := net.JoinHostPort("localhost", port)

	blocked := (&CIMDFetcher{}).ssrfTransport(nil).(*http.Transport)
	if _, err := blocked.DialContext(
		t.Context(),
		"tcp",
		address,
	); !errors.Is(
		err,
		ErrCIMDSSRFBlocked,
	) {
		t.Errorf("dialling %s = %v, want ErrCIMDSSRFBlocked", address, err)
	}

	allowed := (&CIMDFetcher{AllowLoopback: true}).ssrfTransport(nil).(*http.Transport)

	conn, err := allowed.DialContext(t.Context(), "tcp", address)
	if err != nil {
		t.Fatalf("dialling %s with loopback allowed: %v", address, err)
	}
	_ = conn.Close()
}

// logo_uri is rendered on the consent page.
func TestCIMDRequiresAnHTTPSLogo(t *testing.T) {
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/only-known-uri-schemes",
	)

	for logo, wantErr := range map[string]bool{
		"https://example.com/logo.png": false,
		"http://example.com/logo.png":  true,
		"javascript:alert(1)":          true,
	} {
		err := fetchServed(t, func(id string, w http.ResponseWriter, _ *http.Request) {
			meta := validDocument(id)
			meta.LogoURI = logo
			serveMetadata("", meta)(w, nil)
		})

		if got := errors.Is(err, ErrCIMDInvalidURL); got != wantErr {
			t.Errorf("logo_uri %q: err = %v, want refused=%v", logo, err, wantErr)
		}
	}
}

func TestCIMDRefusesAnHTMLDocument(t *testing.T) {
	err := fetchServed(t, func(id string, w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = json.NewEncoder(w).Encode(validDocument(id))
	})
	if err == nil || !strings.Contains(err.Error(), "Content-Type") {
		t.Fatalf("err = %v, want the Content-Type refused", err)
	}
}

func TestCIMDAcceptsADocumentExactlyAtTheSizeCap(t *testing.T) {
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/response-size-limited",
	)

	err := fetchServed(t, func(id string, w http.ResponseWriter, _ *http.Request) {
		body, _ := json.Marshal(validDocument(id))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(append(body, strings.Repeat(" ", cimdMaxBodyBytes-len(body))...))
	})
	if err != nil {
		t.Fatalf("a document of exactly %d bytes was refused: %v", cimdMaxBodyBytes, err)
	}
}

func TestCIMDDoesNotFollowRedirects(t *testing.T) {
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/redirects-not-followed",
	)

	err := fetchServed(t, func(id string, w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("moved") == "" {
			http.Redirect(w, r, "/client?moved=1", http.StatusFound)
			return
		}

		serveMetadata("", validDocument(id))(w, r)
	})
	if err == nil {
		t.Fatal("a redirect to a valid document was followed")
	}
}

func TestCIMDRefusesDotSegments(t *testing.T) {
	spec.Satisfies(
		t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/client-id-has-no-dot-segments",
	)

	f := &CIMDFetcher{}

	for _, clientID := range []string{
		"https://example.com/./client",
		"https://example.com/a/../client",
		"https://example.com/client/..",
		"https://example.com/%2e%2e/client",
	} {
		if err := f.validateURL(clientID); !errors.Is(err, ErrCIMDInvalidURL) {
			t.Errorf("%s: err = %v, want ErrCIMDInvalidURL", clientID, err)
		}
	}

	if err := f.validateURL("https://example.com/.well-known/client.json"); err != nil {
		t.Errorf("a dot inside a segment was refused: %v", err)
	}
}

// RFC 6890's blocks that are not globally reachable, beyond the categories
// net.IP names: none of them is a host a client's metadata can live on.
func TestCIMDRefusesSpecialUseAddresses(t *testing.T) {
	spec.Satisfies(
		t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/special-use-addresses-not-fetched",
	)

	f := &CIMDFetcher{}

	for _, addr := range []string{
		"0.1.2.3", "192.0.0.8", "192.0.2.1", "198.18.0.1", "198.51.100.1",
		"203.0.113.1", "240.0.0.1", "255.255.255.255",
		"100::1", "2001::1", "2001:2::1", "2001:db8::1",
	} {
		if err := f.checkIP(net.ParseIP(addr)); !errors.Is(err, ErrCIMDSSRFBlocked) {
			t.Errorf("checkIP(%s) = %v, want ErrCIMDSSRFBlocked", addr, err)
		}
	}

	for _, addr := range []string{"93.184.216.34", "2606:4700::1111"} {
		if err := f.checkIP(net.ParseIP(addr)); err != nil {
			t.Errorf("checkIP(%s) = %v, want a public address allowed", addr, err)
		}
	}
}

// HTTPS://host/client names the same resource after normalisation, and is
// still not the client_id that was fetched.
func TestCIMDComparesTheClientIDAsAString(t *testing.T) {
	spec.Satisfies(
		t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/client-id-urls-compared-as-strings",
		"oauth/draft-ietf-oauth-client-id-metadata-document/document-client-id-matches-the-fetched-url",
	)

	err := fetchServed(t, func(id string, w http.ResponseWriter, r *http.Request) {
		serveMetadata("", validDocument(strings.Replace(id, "https://", "HTTPS://", 1)))(w, r)
	})
	if !errors.Is(err, ErrCIMDClientIDMismatch) {
		t.Fatalf("err = %v, want ErrCIMDClientIDMismatch", err)
	}
}

// A failure cached would refuse the client until it lapsed, and would let one
// bad answer from its host stand in for every later one.
func TestCIMDCachesNoFailedFetch(t *testing.T) {
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/error-responses-not-cached",
		"oauth/draft-ietf-oauth-client-id-metadata-document/invalid-documents-not-cached",
	)

	for _, tc := range []struct {
		name string
		fail func(w http.ResponseWriter, r *http.Request)
	}{
		{
			name: "an error response",
			fail: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			},
		},
		{
			name: "an invalid document",
			fail: func(w http.ResponseWriter, r *http.Request) {
				serveMetadata("https://elsewhere.example/client", ClientMetadata{})(w, r)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				serverURL string
				calls     atomic.Int32
			)

			srv := httptest.NewTLSServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if calls.Add(1) == 1 {
						tc.fail(w, r)
						return
					}

					serveMetadata("", validDocument(serverURL+"/client"))(w, r)
				}),
			)
			t.Cleanup(srv.Close)
			serverURL = srv.URL

			f := &CIMDFetcher{Client: srv.Client(), AllowLoopback: true}

			if _, err := f.Fetch(t.Context(), srv.URL+"/client"); err == nil {
				t.Fatal("the first fetch succeeded; the case does not fail")
			}

			if _, err := f.Fetch(t.Context(), srv.URL+"/client"); err != nil {
				t.Errorf("the second fetch was answered from the failure: %v", err)
			}
		})
	}
}

// Nothing at the token endpoint authenticates a client, so a document that
// asks to be authenticated is refused rather than treated as public.
func TestCIMDRefusesAuthenticatedClientDocuments(t *testing.T) {
	spec.Satisfies(
		t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/private-key-jwt-client-is-authenticated",
	)

	for _, method := range []string{"private_key_jwt", "tls_client_auth"} {
		err := fetchServed(t, func(id string, w http.ResponseWriter, r *http.Request) {
			meta := validDocument(id)
			meta.TokenEndpointAuthMethod = method
			serveMetadata("", meta)(w, r)
		})
		if !errors.Is(err, ErrCIMDUnsupportedAuth) {
			t.Errorf("%s: err = %v, want ErrCIMDUnsupportedAuth", method, err)
		}
	}

	for _, method := range []string{"", "none"} {
		err := fetchServed(t, func(id string, w http.ResponseWriter, r *http.Request) {
			meta := validDocument(id)
			meta.TokenEndpointAuthMethod = method
			serveMetadata("", meta)(w, r)
		})
		if err != nil {
			t.Errorf("%q: a public client document was refused: %v", method, err)
		}
	}
}

func TestCIMDCacheEntryLapsesAtItsTTL(t *testing.T) {
	spec.Satisfies(t,
		"oauth/draft-ietf-oauth-client-id-metadata-document/client-metadata-is-re-fetched",
		"oauth/draft-ietf-oauth-client-id-metadata-document/cache-headers-respected",
	)

	fetched := time.Unix(1_700_000_000, 0)
	entry := cachedEntry{meta: &ClientMetadata{}, fetchedAt: fetched}

	if !entry.lapsed(fetched.Add(cimdCacheTTL)) {
		t.Error("an entry exactly cimdCacheTTL old is still fresh")
	}

	f := &CIMDFetcher{cache: map[string]cachedEntry{"https://example.com/client": entry}}
	if f.cacheGet("https://example.com/client") != nil {
		t.Error("a lapsed entry was served from the cache")
	}
}
