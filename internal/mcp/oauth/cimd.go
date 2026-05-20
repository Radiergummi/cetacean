package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// cimdCacheTTL is how long a successfully fetched metadata document is cached.
// Expired entries are evicted on the next read; there is no background sweep.
const cimdCacheTTL = time.Hour

// cimdMaxRedirects is the maximum number of redirects the fetcher will follow.
// Lower than the net/http default of 10 to bound per-fetch work.
const cimdMaxRedirects = 5

// cimdMaxBodyBytes is the hard cap on the response body (5 KiB).
const cimdMaxBodyBytes = 5 * 1024

// Sentinel errors for CIMD fetch failures. All are wrapped with %w so callers
// can use errors.Is for fine-grained handling.
var (
	// ErrCIMDInvalidURL is returned when client_id is not a valid HTTPS URL,
	// contains a fragment, carries userinfo, or has a trivial path.
	ErrCIMDInvalidURL = errors.New("CIMD: invalid client_id URL")

	// ErrCIMDSSRFBlocked is returned when DNS resolution yields a private,
	// loopback, link-local, or otherwise unsafe IP address.
	ErrCIMDSSRFBlocked = errors.New("CIMD: SSRF protection blocked request")

	// ErrCIMDOversize is returned when the server's response body exceeds
	// cimdMaxBodyBytes.
	ErrCIMDOversize = errors.New("CIMD: response body too large")

	// ErrCIMDClientIDMismatch is returned when the document's client_id field
	// does not match the requested URL byte-for-byte.
	ErrCIMDClientIDMismatch = errors.New("CIMD: client_id in document does not match requested URL")

	// ErrCIMDSymmetricAuth is returned when the document advertises a symmetric
	// token endpoint authentication method (client_secret_post or
	// client_secret_basic) that Cetacean does not support.
	ErrCIMDSymmetricAuth = errors.New("CIMD: symmetric token_endpoint_auth_method not supported")
)

// ClientMetadata holds the fields from an OAuth Client ID Metadata Document
// (draft-ietf-oauth-client-id-metadata-document).
type ClientMetadata struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

// HasRedirectURI reports whether uri is among the registered redirect URIs.
// Comparison is exact (byte-for-byte); no canonicalization is performed beyond
// what the JSON decoder already applied.
func (m *ClientMetadata) HasRedirectURI(uri string) bool {
	for _, registered := range m.RedirectURIs {
		if registered == uri {
			return true
		}
	}
	return false
}

// cachedEntry is a single in-memory cache slot.
type cachedEntry struct {
	meta      *ClientMetadata
	fetchedAt time.Time
}

// CIMDFetcher fetches and validates OAuth Client ID Metadata Documents.
// The zero value is usable but will use a package-internal HTTP client.
// For tests, set AllowLoopback to true and supply the test server's Client().
type CIMDFetcher struct {
	// Client is the HTTP client used for requests. If nil, a package-internal
	// client with explicit timeouts is used.
	Client *http.Client

	// AllowLoopback disables the loopback-address check in the SSRF guard.
	// Set to true only in tests (httptest servers bind to 127.0.0.1).
	AllowLoopback bool

	mu    sync.RWMutex
	cache map[string]cachedEntry
}

// defaultCIMDClient is used when CIMDFetcher.Client is nil.
var defaultCIMDClient = &http.Client{
	Timeout: 10 * time.Second,
}

// httpClient returns an HTTP client with the SSRF-aware redirect policy applied.
// It shallow-copies the base client so the caller's client is not mutated.
// The Transport is shared (intentional — preserves TLS session caching).
func (f *CIMDFetcher) httpClient() *http.Client {
	base := f.Client
	if base == nil {
		base = defaultCIMDClient
	}

	c := *base // shallow copy
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= cimdMaxRedirects {
			return fmt.Errorf("CIMD: exceeded %d redirects", cimdMaxRedirects)
		}
		// Re-validate every redirect target through the full URL + SSRF pipeline.
		// NOTE: DNS resolution here uses the live hostname from the redirect
		// Location header, so an attacker cannot sneak in a private IP via a
		// redirect chain.
		if err := f.validateURL(req.URL.String()); err != nil {
			return err
		}
		if err := f.checkSSRF(req.URL.Hostname()); err != nil {
			return err
		}
		return nil
	}
	return &c
}

// Fetch retrieves the Client ID Metadata Document for the given client_id URL.
// It validates the URL, applies SSRF guards, enforces a size cap, and checks
// structural invariants in the returned document. Successful results are cached
// for cimdCacheTTL; failed fetches are never cached.
func (f *CIMDFetcher) Fetch(ctx context.Context, clientID string) (*ClientMetadata, error) {
	// Step 1–2: validate URL structure (scheme, path, fragment, userinfo).
	if err := f.validateURL(clientID); err != nil {
		return nil, err
	}

	// Step 3: cache hit check.
	if meta := f.cacheGet(clientID); meta != nil {
		return meta, nil
	}

	// Step 4: SSRF DNS guard — resolve before any TCP connection is made.
	u, _ := parseHTTPSURL(clientID) // already validated above; error is impossible here
	if err := f.checkSSRF(u.Hostname()); err != nil {
		return nil, err
	}

	// Step 5: build the request with a 5-second per-fetch timeout layered on
	// top of any parent context deadline.
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, fmt.Errorf("CIMD fetch: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	// Steps 5–6: execute (redirect SSRF re-validation is wired in httpClient()).
	resp, err := f.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("CIMD fetch: HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Step 7: only 200 OK is acceptable.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CIMD fetch: unexpected HTTP status %d", resp.StatusCode)
	}

	// Step 9: content-type advisory check.
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		if strings.HasPrefix(ct, "text/html") {
			return nil, fmt.Errorf("CIMD fetch: unexpected Content-Type %q (expected application/json)", ct)
		}
	}

	// Step 8: size-cap. Read at most cimdMaxBodyBytes+1 so we can distinguish
	// "exactly at limit" from "over limit".
	limited := io.LimitReader(resp.Body, int64(cimdMaxBodyBytes)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("CIMD fetch: read body: %w", err)
	}
	if len(body) > cimdMaxBodyBytes {
		return nil, fmt.Errorf("%w: response exceeds %d bytes", ErrCIMDOversize, cimdMaxBodyBytes)
	}

	// Step 10: JSON decode.
	var meta ClientMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("CIMD fetch: JSON decode: %w", err)
	}

	// Step 11: client_id mirroring — the document MUST echo the requested URL.
	if meta.ClientID != clientID {
		return nil, fmt.Errorf("%w: document has %q, requested %q",
			ErrCIMDClientIDMismatch, meta.ClientID, clientID)
	}

	// Step 12: reject symmetric token endpoint auth methods.
	if isSymmetricAuthMethod(meta.TokenEndpointAuthMethod) {
		return nil, fmt.Errorf("%w: %q", ErrCIMDSymmetricAuth, meta.TokenEndpointAuthMethod)
	}

	// Cache on success only. Negative results are never cached so an attacker
	// cannot poison the cache with a single failed fetch.
	f.cachePut(clientID, &meta)

	return &meta, nil
}

// validateURL checks structural requirements for a CIMD client_id URL:
// HTTPS scheme, non-trivial path, no fragment, no userinfo.
func (f *CIMDFetcher) validateURL(rawURL string) error {
	u, err := parseHTTPSURL(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCIMDInvalidURL, err)
	}
	if u.Fragment != "" {
		return fmt.Errorf("%w: URL must not contain a fragment", ErrCIMDInvalidURL)
	}
	if u.User != nil {
		return fmt.Errorf("%w: URL must not contain userinfo credentials", ErrCIMDInvalidURL)
	}
	if u.Path == "" || u.Path == "/" {
		return fmt.Errorf("%w: URL must have a non-trivial path component", ErrCIMDInvalidURL)
	}
	return nil
}

// parseHTTPSURL parses rawURL and returns an error if the scheme is not https.
func parseHTTPSURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse: %v", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("scheme must be https, got %q", u.Scheme)
	}
	return u, nil
}

// checkSSRF resolves hostname and rejects addresses in the SSRF block-list.
func (f *CIMDFetcher) checkSSRF(hostname string) error {
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("CIMD fetch: DNS lookup %q: %w", hostname, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("%w: no addresses resolved for %q", ErrCIMDSSRFBlocked, hostname)
	}
	for _, ip := range ips {
		if err := f.checkIP(ip); err != nil {
			return err
		}
	}
	return nil
}

// checkIP validates a single resolved IP against the SSRF block-list.
// The block-list is exhaustive: loopback (unless AllowLoopback), private
// (RFC 1918 + ULA), link-local (unicast + multicast), unspecified, and
// multicast addresses are all rejected.
func (f *CIMDFetcher) checkIP(ip net.IP) error {
	if ip.IsLoopback() && !f.AllowLoopback {
		return fmt.Errorf("%w: resolved to loopback address %s", ErrCIMDSSRFBlocked, ip)
	}
	if ip.IsPrivate() {
		return fmt.Errorf("%w: resolved to private address %s", ErrCIMDSSRFBlocked, ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("%w: resolved to link-local address %s", ErrCIMDSSRFBlocked, ip)
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("%w: resolved to unspecified address %s", ErrCIMDSSRFBlocked, ip)
	}
	if ip.IsMulticast() {
		return fmt.Errorf("%w: resolved to multicast address %s", ErrCIMDSSRFBlocked, ip)
	}
	return nil
}

// isSymmetricAuthMethod reports whether the given token_endpoint_auth_method
// is one of the symmetric methods (client_secret_post, client_secret_basic)
// that Cetacean does not support.
func isSymmetricAuthMethod(method string) bool {
	return method == "client_secret_post" || method == "client_secret_basic"
}

// cacheGet returns a cached metadata entry if one exists and is still fresh.
func (f *CIMDFetcher) cacheGet(clientID string) *ClientMetadata {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.cache == nil {
		return nil
	}
	entry, ok := f.cache[clientID]
	if !ok || time.Since(entry.fetchedAt) >= cimdCacheTTL {
		return nil
	}
	return entry.meta
}

// cachePut stores a metadata entry. Only called on successful fetches.
func (f *CIMDFetcher) cachePut(clientID string, meta *ClientMetadata) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cache == nil {
		f.cache = make(map[string]cachedEntry)
	}
	f.cache[clientID] = cachedEntry{meta: meta, fetchedAt: time.Now()}
}
