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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/radiergummi/cetacean/internal/netguard"
)

// cimdDialer is the shared net.Dialer used by the default CIMD transport for
// individual IP dials. The transport's per-host timeout caps total connect
// time, so the dialer just needs reasonable per-attempt budgets.
var cimdDialer = &net.Dialer{
	Timeout:   2 * time.Second,
	KeepAlive: 30 * time.Second,
}

// cimdCacheTTL is how long a document is cached when its response says
// nothing about it. Expired entries are evicted on the next read; there is no
// background sweep.
const cimdCacheTTL = time.Hour

// cimdMaxCacheTTL caps the lifetime a response asks for, so a document is
// re-read at least daily however long its publisher says it stays fresh.
const cimdMaxCacheTTL = 24 * time.Hour

// cimdCacheMaxEntries bounds the cache. client_id is the caller's to choose and
// the document is served by a host they control, so an authorize loop over
// distinct URLs would otherwise grow this map without limit. DCR has
// DCRMaxClients for the same reason; this is CIMD's.
const cimdCacheMaxEntries = 512

// cimdMaxBodyBytes is the hard cap on the response body (5 KiB).
const cimdMaxBodyBytes = 5 * 1024

// cimdFetchTimeout bounds the total per-fetch budget end-to-end.
const cimdFetchTimeout = 5 * time.Second

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

	// ErrCIMDSymmetricAuth is returned when the document advertises a token
	// endpoint authentication method built on a shared secret, which Cetacean
	// does not support.
	ErrCIMDSymmetricAuth = errors.New("CIMD: symmetric token_endpoint_auth_method not supported")

	// ErrCIMDUnsupportedAuth is returned when the document asks to be
	// authenticated at the token endpoint, which authenticates no client.
	ErrCIMDUnsupportedAuth = errors.New("CIMD: token_endpoint_auth_method not supported")

	errCIMDRedirect = errors.New("CIMD: redirects are not followed")
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
	return slices.Contains(m.RedirectURIs, uri)
}

// cachedEntry is a single in-memory cache slot.
type cachedEntry struct {
	meta    *ClientMetadata
	expires time.Time
}

// lapsed reports whether the entry has aged out as of now.
func (e cachedEntry) lapsed(now time.Time) bool {
	return !now.Before(e.expires)
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

	// clientOnce builds the SSRF-aware http.Client lazily on first fetch.
	// Once built it is reused across every Fetch call so connection pooling,
	// idle timeouts, and the DialContext closure aren't rebuilt per request.
	// Mutating Client / AllowLoopback after the first Fetch has no effect.
	clientOnce sync.Once
	cachedHTTP *http.Client

	mu    sync.RWMutex
	cache map[string]cachedEntry
}

// httpClient returns an HTTP client suitable for fetching CIMD documents. Its
// DialContext resolves the host, screens the IP and connects to that exact
// address in one step, so a DNS-rebinding attacker cannot answer the
// validation lookup and the connect differently. Redirects are never followed.
func (f *CIMDFetcher) httpClient() *http.Client {
	f.clientOnce.Do(func() {
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
		c.CheckRedirect = func(*http.Request, []*http.Request) error {
			return errCIMDRedirect
		}
		f.cachedHTTP = &c
	})
	return f.cachedHTTP
}

// ssrfTransport returns an http.RoundTripper performing SSRF-aware dialing. A
// base *http.Transport is shallow-cloned rather than mutated. The DialContext
// resolves the host, screens every IP through checkIP and dials the first
// survivor, pinning the connection so net/http cannot resolve again unchecked.
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
	t.DialContext = netguard.DialContext(
		cimdDialer,
		func(_ context.Context, _ string, ip net.IP) error { return f.checkIP(ip) },
	)
	return t
}

// Fetch retrieves the Client ID Metadata Document for a client_id URL: it
// validates the URL, guards against SSRF in the dialer, caps the size and
// checks the document's structural invariants. Successful results are cached
// for as long as their response allows; failed fetches never are.
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
	// transport's DialContext; a redirect fails the fetch.
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
			return nil, fmt.Errorf(
				"CIMD fetch: unexpected Content-Type %q (expected application/json)",
				ct,
			)
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
	if m := meta.TokenEndpointAuthMethod; m != "" && m != "none" {
		return nil, fmt.Errorf("%w: %q", ErrCIMDUnsupportedAuth, m)
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

	// Step 14: logo_uri must be https when present. Nothing renders it: the
	// consent page loads no third-party resources, so passing this check does
	// not make the URI safe to embed.
	if meta.LogoURI != "" {
		u, err := url.Parse(meta.LogoURI)
		if err != nil || u.Scheme != "https" {
			return nil, fmt.Errorf("%w: logo_uri must be https, got %q",
				ErrCIMDInvalidURL, meta.LogoURI)
		}
	}

	// Cache on success only. Negative results are never cached so an attacker
	// cannot poison the cache with a single failed fetch.
	f.cachePut(clientID, &meta, cacheLifetime(resp.Header, time.Now()))

	return &meta, nil
}

// validateURL checks structural requirements for a CIMD client_id URL:
// HTTPS scheme, non-trivial path, no fragment, no userinfo.
func (f *CIMDFetcher) validateURL(rawURL string) error {
	u, err := parseHTTPSURL(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCIMDInvalidURL, err)
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
	if slices.ContainsFunc(strings.Split(u.Path, "/"), isDotSegment) {
		return fmt.Errorf("%w: URL must not contain dot segments", ErrCIMDInvalidURL)
	}
	return nil
}

// isDotSegment reports whether a path segment is "." or "..". u.Path is
// already decoded, so the percent-encoded spellings are refused too.
func isDotSegment(segment string) bool {
	return segment == "." || segment == ".."
}

// parseHTTPSURL parses rawURL and returns an error if the scheme is not https.
func parseHTTPSURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("scheme must be https, got %q", u.Scheme)
	}
	return u, nil
}

// checkIP validates a single resolved IP against the SSRF block-list.
// The block-list is exhaustive: loopback (unless AllowLoopback), private
// (RFC 1918 + ULA), link-local (unicast + multicast), unspecified, multicast
// and the rest of RFC 6890's special-use blocks are all rejected.
func (f *CIMDFetcher) checkIP(ip net.IP) error {
	if err := (netguard.Policy{AllowLoopback: f.AllowLoopback}).Check(ip); err != nil {
		return fmt.Errorf("%w: %w", ErrCIMDSSRFBlocked, err)
	}
	return nil
}

// isSymmetricAuthMethod reports whether the given token_endpoint_auth_method
// is one of the registered methods built on a shared secret, which a client
// that registers itself by URL has no way to establish.
func isSymmetricAuthMethod(method string) bool {
	switch method {
	case "client_secret_post", "client_secret_basic", "client_secret_jwt":
		return true
	default:
		return false
	}
}

// cacheGet returns a cached metadata entry if one exists and is still fresh.
func (f *CIMDFetcher) cacheGet(clientID string) *ClientMetadata {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.cache == nil {
		return nil
	}
	entry, ok := f.cache[clientID]
	if !ok || entry.lapsed(time.Now()) {
		return nil
	}
	return entry.meta
}

// cachePut stores a metadata entry for lifetime, or drops the client's entry
// when lifetime is not positive. Only called on successful fetches.
func (f *CIMDFetcher) cachePut(clientID string, meta *ClientMetadata, lifetime time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if lifetime <= 0 {
		delete(f.cache, clientID)
		return
	}
	if f.cache == nil {
		f.cache = make(map[string]cachedEntry)
	}

	now := time.Now()
	if _, replacing := f.cache[clientID]; !replacing {
		f.evictFor(now)
	}

	f.cache[clientID] = cachedEntry{meta: meta, expires: now.Add(lifetime)}
}

// cacheLifetime is how long a response may stay cached, read as a shared cache
// reads it: none at all under no-store, no-cache or private, else s-maxage,
// max-age or Expires less Date, less Age. cimdCacheTTL stands in when the
// headers name no lifetime, and cimdMaxCacheTTL caps any they do.
func cacheLifetime(h http.Header, now time.Time) time.Duration {
	directives := cacheControl(h)

	for _, name := range []string{"no-store", "no-cache", "private"} {
		// A qualified no-cache or private only withholds the fields it names.
		if value, ok := directives[name]; ok && value == "" {
			return 0
		}
	}

	var lifetime time.Duration

	if value, ok := directives["s-maxage"]; ok {
		lifetime = deltaSeconds(value)
	} else if value, ok := directives["max-age"]; ok {
		lifetime = deltaSeconds(value)
	} else if values := h.Values("Expires"); len(values) > 0 {
		date, err := http.ParseTime(h.Get("Date"))
		if err != nil {
			date = now
		}

		// An Expires that does not parse is a time in the past.
		if expires, err := http.ParseTime(values[0]); err == nil {
			lifetime = expires.Sub(date)
		}
	} else {
		lifetime = cimdCacheTTL
	}

	lifetime -= deltaSeconds(h.Get("Age"))

	return min(max(lifetime, 0), cimdMaxCacheTTL)
}

// cacheControl folds every Cache-Control line into its directives, names
// lowercased and quotes stripped. The first occurrence of a directive wins.
func cacheControl(h http.Header) map[string]string {
	directives := make(map[string]string)

	for _, line := range h.Values("Cache-Control") {
		for directive := range strings.SplitSeq(line, ",") {
			name, value, _ := strings.Cut(strings.TrimSpace(directive), "=")
			name = strings.ToLower(strings.TrimSpace(name))
			if _, seen := directives[name]; name == "" || seen {
				continue
			}

			directives[name] = strings.Trim(strings.TrimSpace(value), `"`)
		}
	}

	return directives
}

// deltaSeconds parses a delta-seconds value. One past 2^31 saturates there
// rather than fails, and one that is not a number at all reads as zero.
func deltaSeconds(value string) time.Duration {
	seconds, err := strconv.ParseUint(value, 10, 31)
	if errors.Is(err, strconv.ErrRange) {
		return 1 << 31 * time.Second
	}
	if err != nil {
		return 0
	}

	return time.Duration(seconds) * time.Second
}

// evictFor makes room for one new entry, called with the mutex held. Lapsed
// entries go first, since dropping them costs nothing; only if the cache is
// still full does a live one go, the soonest to lapse. A re-fetch costs an
// evicted client one request, so the cap trades a cold start for a bounded map.
func (f *CIMDFetcher) evictFor(now time.Time) {
	if len(f.cache) < cimdCacheMaxEntries {
		return
	}

	for id, entry := range f.cache {
		if entry.lapsed(now) {
			delete(f.cache, id)
		}
	}

	// One eviction is enough: the cap is a constant and this runs before every
	// insert, so the map is never more than one entry over it.
	if len(f.cache) < cimdCacheMaxEntries {
		return
	}

	soonestID := ""
	var soonest time.Time
	for id, entry := range f.cache {
		if soonestID == "" || entry.expires.Before(soonest) {
			soonestID, soonest = id, entry.expires
		}
	}

	delete(f.cache, soonestID)
}
