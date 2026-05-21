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

// cimdDialer is the shared net.Dialer used by the default CIMD transport for
// individual IP dials. The transport's per-host timeout caps total connect
// time, so the dialer just needs reasonable per-attempt budgets.
var cimdDialer = &net.Dialer{
	Timeout:   2 * time.Second,
	KeepAlive: 30 * time.Second,
}

// cimdCacheTTL is how long a successfully fetched metadata document is cached.
// Expired entries are evicted on the next read; there is no background sweep.
const cimdCacheTTL = time.Hour

// cimdMaxRedirects is the maximum number of redirects the fetcher will follow.
// Lower than the net/http default of 10 to bound per-fetch work.
const cimdMaxRedirects = 5

// cimdMaxBodyBytes is the hard cap on the response body (5 KiB).
const cimdMaxBodyBytes = 5 * 1024

// cimdFetchTimeout bounds the total per-fetch budget end-to-end.
const cimdFetchTimeout = 5 * time.Second

// cgnatBlock is RFC 6598 (100.64.0.0/10) — carrier-grade NAT and Tailscale's
// default address pool. net.IP.IsPrivate covers RFC 1918 and ULA but not
// CGNAT, so we add it explicitly.
var cgnatBlock = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("100.64.0.0/10")
	return n
}()

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

// httpClient returns an HTTP client suitable for fetching CIMD documents.
//
// When f.Client is nil (the production path) the returned client uses a
// Transport whose DialContext resolves the host, validates the resulting IP
// against the SSRF block-list, and connects to that exact IP — collapsing the
// previous "pre-flight LookupIP + transport LookupIP" pair into a single
// resolution so a DNS-rebinding attacker cannot return a public IP for the
// validation lookup and a private IP for the actual connect.
//
// When f.Client is non-nil (tests inject httptest.Server.Client()) the caller's
// client is shallow-copied. Tests that need to reach loopback set
// AllowLoopback=true to skip the IP check; in production that flag should
// remain false.
//
// CheckRedirect re-runs URL-structure validation (https, no userinfo, no
// fragment, non-trivial path). DNS validation for the redirect target happens
// in the same DialContext on the follow-up request.
func (f *CIMDFetcher) httpClient() *http.Client {
	var c http.Client
	if f.Client != nil {
		c = *f.Client
		if c.Transport == nil {
			c.Transport = f.ssrfTransport(http.DefaultTransport)
		} else {
			c.Transport = f.ssrfTransport(c.Transport)
		}
	} else {
		c = http.Client{
			Timeout:   cimdFetchTimeout,
			Transport: f.ssrfTransport(nil),
		}
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= cimdMaxRedirects {
			return fmt.Errorf("CIMD: exceeded %d redirects", cimdMaxRedirects)
		}
		return f.validateURL(req.URL.String())
	}
	return &c
}

// ssrfTransport returns an http.RoundTripper that performs SSRF-aware dialing.
// If base is an *http.Transport, it is shallow-cloned so we don't mutate the
// caller's transport. If base is nil, a fresh Transport is built with the
// same defaults net/http uses.
//
// The custom DialContext resolves the host, screens every returned IP through
// checkIP, and dials the first survivor — pinning the connection to the
// validated address so net/http cannot perform a second, unvalidated DNS
// lookup.
func (f *CIMDFetcher) ssrfTransport(base http.RoundTripper) http.RoundTripper {
	var t *http.Transport
	if base == nil {
		t = &http.Transport{
			TLSHandshakeTimeout:   2 * time.Second,
			ResponseHeaderTimeout: 3 * time.Second,
			MaxIdleConns:          16,
			IdleConnTimeout:       30 * time.Second,
		}
	} else if existing, ok := base.(*http.Transport); ok {
		clone := existing.Clone()
		t = clone
	} else {
		// Non-transport RoundTripper (e.g. test stub) — return it unchanged.
		// Such callers are responsible for their own SSRF protections.
		return base
	}
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("CIMD dial: invalid address %q: %w", address, err)
		}
		// If the host is already an IP literal, validate it directly.
		if ip := net.ParseIP(host); ip != nil {
			if err := f.checkIP(ip); err != nil {
				return nil, err
			}
			return cimdDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("CIMD dial: lookup %q: %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("%w: no addresses resolved for %q", ErrCIMDSSRFBlocked, host)
		}
		var firstErr error
		for _, ip := range ips {
			if err := f.checkIP(ip); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			conn, dialErr := cimdDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			if firstErr == nil {
				firstErr = dialErr
			}
		}
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("CIMD dial: no usable addresses for %q", host)
	}
	return t
}

// Fetch retrieves the Client ID Metadata Document for the given client_id URL.
// It validates the URL, applies SSRF guards in the dialer, enforces a size cap,
// and checks structural invariants in the returned document. Successful results
// are cached for cimdCacheTTL; failed fetches are never cached.
//
// SSRF protection runs in the HTTP client's DialContext (see httpClient /
// ssrfTransport) so DNS resolution and IP validation happen as a single atomic
// step — no pre-flight LookupIP + transport LookupIP TOCTOU window.
func (f *CIMDFetcher) Fetch(ctx context.Context, clientID string) (*ClientMetadata, error) {
	// Step 1–2: validate URL structure (scheme, path, fragment, userinfo).
	if err := f.validateURL(clientID); err != nil {
		return nil, err
	}

	// Step 3: cache hit check.
	if meta := f.cacheGet(clientID); meta != nil {
		return meta, nil
	}

	// Step 4: build the request with a 5-second per-fetch timeout layered on
	// top of any parent context deadline.
	fetchCtx, cancel := context.WithTimeout(ctx, cimdFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, fmt.Errorf("CIMD fetch: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	// Steps 5–6: execute. SSRF block-list enforcement happens inside the
	// transport's DialContext; redirects re-run URL-structure validation in
	// CheckRedirect and then dial through the same SSRF-aware path.
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

	// Step 13: validate redirect_uris. Reuse the DCR validator so CIMD and
	// dynamically-registered clients converge on the same allowed shapes
	// (https or loopback http). Rejects schemes like "javascript:" that
	// would otherwise pass HasRedirectURI's string-equality match.
	for _, uri := range meta.RedirectURIs {
		if !isValidRedirectURI(uri) {
			return nil, fmt.Errorf("%w: invalid redirect_uri %q (must be https or loopback http)",
				ErrCIMDInvalidURL, uri)
		}
	}

	// Step 14: logo_uri must be https when present — it's rendered on the
	// consent page so other schemes are an XSS / data-exfil vector.
	if meta.LogoURI != "" {
		u, err := url.Parse(meta.LogoURI)
		if err != nil || u.Scheme != "https" {
			return nil, fmt.Errorf("%w: logo_uri must be https, got %q",
				ErrCIMDInvalidURL, meta.LogoURI)
		}
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
	if cgnatBlock.Contains(ip) {
		return fmt.Errorf("%w: resolved to CGNAT address %s", ErrCIMDSSRFBlocked, ip)
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
