package oauth

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// dcrMaxBodyBytes caps the size of a DCR registration request body. RFC 7591
// places no normative limit, but real registration metadata fits in a few KiB;
// 64 KiB leaves comfortable headroom for unusual but legitimate inputs while
// preventing a single client from forcing the server to buffer megabytes.
const dcrMaxBodyBytes = 64 * 1024

// ClientRegistration holds a dynamically registered OAuth client.
type ClientRegistration struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
}

// dcrRequest is the incoming JSON body for RFC 7591 registration.
type dcrRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// ipBucket is a per-IP rate limiter bucket using a simple token bucket approach.
type ipBucket struct {
	tokens    int
	resetAt   time.Time
	windowSec int
}

// ClientRegistry stores dynamically registered clients with LRU eviction.
type ClientRegistry struct {
	mu         sync.Mutex
	clients    map[string]*ClientRegistration
	order      []string // insertion-order for LRU eviction (oldest first)
	maxClients int

	rateMu    sync.Mutex
	buckets   map[string]*ipBucket
	rateLimit int // requests per hour per IP
}

// newClientRegistry creates a new registry with the given capacity and rate limit.
func newClientRegistry(maxClients, rateLimit int) *ClientRegistry {
	return &ClientRegistry{
		clients:    make(map[string]*ClientRegistration),
		buckets:    make(map[string]*ipBucket),
		maxClients: maxClients,
		rateLimit:  rateLimit,
	}
}

// Get retrieves a client by ID. Returns nil if not found.
func (r *ClientRegistry) Get(clientID string) *ClientRegistration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.clients[clientID]
}

// register adds a client, evicting the oldest if at capacity.
func (r *ClientRegistry) register(reg *ClientRegistration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Evict oldest if at capacity.
	if len(r.clients) >= r.maxClients && r.maxClients > 0 {
		if len(r.order) > 0 {
			oldest := r.order[0]
			r.order = r.order[1:]
			delete(r.clients, oldest)
		}
	}

	r.clients[reg.ClientID] = reg
	r.order = append(r.order, reg.ClientID)
}

// checkRateLimit returns true if the IP is allowed to make a registration request.
//
// Inline sweep: every call evicts expired buckets while the lock is already
// held. Without this, the map grows unbounded for the lifetime of the process —
// every distinct source IP (scanners, NATed clients, rotating proxies) leaves
// a permanent entry. The sweep is O(n) per call but the map stays bounded to
// the active-IP working set.
func (r *ClientRegistry) checkRateLimit(ip string) bool {
	r.rateMu.Lock()
	defer r.rateMu.Unlock()

	now := time.Now()
	for k, b := range r.buckets {
		if now.After(b.resetAt) {
			delete(r.buckets, k)
		}
	}

	bucket, ok := r.buckets[ip]
	if !ok {
		r.buckets[ip] = &ipBucket{
			tokens:    r.rateLimit - 1,
			resetAt:   now.Add(time.Hour),
			windowSec: 3600,
		}
		return true
	}

	if bucket.tokens <= 0 {
		return false
	}
	bucket.tokens--
	return true
}

// isLoopbackURI reports whether rawURI is an HTTP URI targeting localhost or 127.x.
func isLoopbackURI(rawURI string) bool {
	u, err := url.Parse(rawURI)
	if err != nil {
		return false
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}

// isValidRedirectURI reports whether a redirect URI is acceptable for DCR:
// must be https:// or a loopback http:// URI.
func isValidRedirectURI(rawURI string) bool {
	u, err := url.Parse(rawURI)
	if err != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return isLoopbackURI(rawURI)
}

// HandleRegister handles POST {base}/oauth/register (RFC 7591 DCR).
func (s *Server) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if s.clients == nil {
		http.Error(w, "DCR not enabled", http.StatusNotFound)
		return
	}

	// Per-IP rate limiting. r.RemoteAddr is the real client IP when the
	// server.trusted_proxies allowlist is configured — the realIP middleware
	// (internal/api/realip.go) rewrites it from X-Forwarded-For before this
	// handler runs. Behind a reverse proxy with no trusted_proxies set, every
	// caller shares the proxy's bucket; document this dependency for operators.
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if !s.clients.checkRateLimit(ip) {
		w.Header().Set("Retry-After", "3600")
		writeDCRError(
			w,
			http.StatusTooManyRequests,
			"too_many_requests",
			"registration rate limit exceeded",
		)
		return
	}

	// Bound the request body to keep this public endpoint from buffering
	// arbitrarily large payloads (trivial DoS otherwise).
	r.Body = http.MaxBytesReader(w, r.Body, dcrMaxBodyBytes)

	// RFC 7591: unknown fields MUST be ignored.
	var req dcrRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDCRError(
			w,
			http.StatusBadRequest,
			"invalid_client_metadata",
			"invalid JSON: "+err.Error(),
		)
		return
	}

	// Validate redirect_uris.
	if len(req.RedirectURIs) == 0 {
		writeDCRError(
			w,
			http.StatusBadRequest,
			"invalid_client_metadata",
			"redirect_uris is required",
		)
		return
	}
	for _, uri := range req.RedirectURIs {
		if !isValidRedirectURI(uri) {
			writeDCRError(w, http.StatusBadRequest, "invalid_client_metadata",
				"redirect_uri must be https:// or loopback http://: "+uri)
			return
		}
	}

	// Validate token_endpoint_auth_method.
	authMethod := req.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "none"
	}
	if isSymmetricAuthMethod(authMethod) {
		writeDCRError(w, http.StatusBadRequest, "invalid_client_metadata",
			"token_endpoint_auth_method must be 'none' (public clients only)")
		return
	}

	// Generate client_id.
	clientID := "cetacean-" + generateOpaqueToken()

	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}
	for _, gt := range grantTypes {
		if gt != "authorization_code" && gt != "refresh_token" {
			writeDCRError(w, http.StatusBadRequest, "invalid_client_metadata",
				"grant_types must be a subset of [authorization_code, refresh_token]")
			return
		}
	}

	responseTypes := req.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}
	for _, rt := range responseTypes {
		if rt != "code" {
			writeDCRError(w, http.StatusBadRequest, "invalid_client_metadata",
				"response_types must be a subset of [code]")
			return
		}
	}

	reg := &ClientRegistration{
		ClientID:                clientID,
		ClientName:              req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              grantTypes,
		ResponseTypes:           responseTypes,
		TokenEndpointAuthMethod: authMethod,
		ClientIDIssuedAt:        time.Now().Unix(),
	}

	body, err := json.Marshal(reg)
	if err != nil {
		writeDCRError(
			w,
			http.StatusInternalServerError,
			"server_error",
			"failed to encode registration",
		)
		return
	}
	s.clients.register(reg)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(body)
}

// dcrErrorResponse is the RFC 7591 error response format.
type dcrErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func writeDCRError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dcrErrorResponse{Error: code, ErrorDescription: desc})
}
