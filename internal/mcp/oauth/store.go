package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

// generateOpaqueToken returns a 32-byte cryptographically random token
// encoded as base64url (no padding). Panics on RNG failure.
func generateOpaqueToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("mcp/oauth: crypto/rand failure: " + err.Error())
	}

	return base64.RawURLEncoding.EncodeToString(b)
}

// hashToken returns the SHA-256 hash of raw encoded as base64url (no padding).
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// AuthCodeStore
// ---------------------------------------------------------------------------

// AuthCodeData carries the claims bound to a single-use authorization code.
type AuthCodeData struct {
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Resource      string // RFC 8707 — the MCP endpoint URL the code is bound to
	Subject       string
	Groups        []string
}

type authCodeEntry struct {
	data      AuthCodeData
	expiresAt time.Time
}

// AuthCodeStore is a short-lived, single-use authorization code store.
// Raw codes are never persisted; only their SHA-256 hashes are stored.
type AuthCodeStore struct {
	mu    sync.Mutex
	codes map[string]authCodeEntry // hash → entry
}

// NewAuthCodeStore returns a ready-to-use AuthCodeStore.
func NewAuthCodeStore() *AuthCodeStore {
	return &AuthCodeStore{
		codes: make(map[string]authCodeEntry),
	}
}

// Issue mints a new authorization code, stores a hash of it, and returns
// the raw code to the caller. The code is valid for ttl from now. Expired
// entries are pruned inline so abandoned flows cannot accumulate.
func (s *AuthCodeStore) Issue(data AuthCodeData, ttl time.Duration) string {
	raw := generateOpaqueToken()
	h := hashToken(raw)
	now := time.Now()

	s.mu.Lock()
	for hash, entry := range s.codes {
		if now.After(entry.expiresAt) {
			delete(s.codes, hash)
		}
	}
	s.codes[h] = authCodeEntry{
		data:      data,
		expiresAt: now.Add(ttl),
	}
	s.mu.Unlock()

	return raw
}

// Redeem looks up the code by hash, always deletes it (single-use), and
// returns the bound data only when the entry existed and was not expired.
// The returned AuthCodeData carries an independent copy of Groups so the
// caller can safely mutate it without racing other holders of the same code.
func (s *AuthCodeStore) Redeem(code string) (AuthCodeData, bool) {
	h := hashToken(code)

	s.mu.Lock()
	entry, exists := s.codes[h]
	delete(s.codes, h)
	s.mu.Unlock()

	if !exists {
		return AuthCodeData{}, false
	}
	if time.Now().After(entry.expiresAt) {
		return AuthCodeData{}, false
	}

	out := entry.data
	out.Groups = append([]string(nil), entry.data.Groups...)
	return out, true
}

// ---------------------------------------------------------------------------
// RefreshTokenStore
// ---------------------------------------------------------------------------

// RefreshTokenData carries the claims bound to a refresh token.
type RefreshTokenData struct {
	Subject  string
	Groups   []string
	ClientID string
	Resource string // RFC 8707 — the MCP endpoint URL this grant is bound to
	grantID  string // assigned and tracked by the store; unexported
}

// RotateResult is returned by RefreshTokenStore.Rotate. On the theft path Data
// names the family that was burned, so the caller can clear its consent record
// and log the affected subject.
type RotateResult struct {
	NewToken string // empty when OK is false
	Data     RefreshTokenData
	OK       bool
	Theft    bool // true when a previously-consumed token was re-presented
}

type refreshTokenEntry struct {
	data RefreshTokenData
	// expiresAt is the per-token expiry. Set on every rotation; capped by
	// grantExpiresAt so sliding refresh cannot extend the grant family past
	// the absolute lifetime set at Issue time.
	expiresAt time.Time
	// grantExpiresAt is the absolute expiry of the grant family. Set once on
	// Issue and carried forward unchanged by Rotate so a daily refresher
	// cannot hold the grant indefinitely.
	grantExpiresAt time.Time
}

// maxGrantHistorySize caps the per-grant rotation history. Theft detection
// works only for replays of tokens within this window; older consumed hashes
// are evicted. Tokens past their TTL would fail to rotate anyway, so the
// effective theft-detection window is min(history, TTL).
const maxGrantHistorySize = 32

// RefreshTokenStore manages long-lived refresh tokens with rotation-on-use
// and grant-family theft detection. Raw tokens are never persisted; only
// their SHA-256 hashes are held in memory.
type RefreshTokenStore struct {
	changeNotifier

	mu       sync.Mutex
	tokens   map[string]refreshTokenEntry // hash → live entry
	consumed map[string]string            // hash → grantID (rotated-away tokens)
	grants   map[string][]string          // grantID → ordered slice of recent hashes (capped at maxGrantHistorySize)
}

// NewRefreshTokenStore returns a ready-to-use RefreshTokenStore.
func NewRefreshTokenStore() *RefreshTokenStore {
	return &RefreshTokenStore{
		tokens:   make(map[string]refreshTokenEntry),
		consumed: make(map[string]string),
		grants:   make(map[string][]string),
	}
}

// Issue mints a new refresh token for a fresh grant family and returns the
// raw token. A unique grantID is stamped onto a copy of data, and the grant's
// absolute expiry is set to now+ttl — subsequent rotations refresh the
// per-token expiry but never extend the grant past this point.
func (s *RefreshTokenStore) Issue(data RefreshTokenData, ttl time.Duration) string {
	raw := generateOpaqueToken()
	h := hashToken(raw)

	grantID := generateOpaqueToken()
	data.grantID = grantID

	expires := time.Now().Add(ttl)

	s.mu.Lock()
	s.tokens[h] = refreshTokenEntry{
		data:           data,
		expiresAt:      expires,
		grantExpiresAt: expires,
	}
	s.grants[grantID] = []string{h}
	s.mu.Unlock()

	s.writeThrough()

	return raw
}

// Validate returns the data for a live, unexpired token without consuming it.
// It is safe to call concurrently without side effects on store state.
// The returned RefreshTokenData carries an independent copy of Groups so the
// caller can safely mutate it without racing other validators.
func (s *RefreshTokenStore) Validate(token string) (RefreshTokenData, bool) {
	h := hashToken(token)

	s.mu.Lock()
	entry, exists := s.tokens[h]
	s.mu.Unlock()

	if !exists {
		return RefreshTokenData{}, false
	}
	if time.Now().After(entry.expiresAt) {
		return RefreshTokenData{}, false
	}

	out := entry.data
	out.Groups = append([]string(nil), entry.data.Groups...)
	return out, true
}

// Rotate implements refresh-token rotation with theft detection.
//
// Algorithm:
//  1. Hash the presented token.
//  2. Replay check FIRST: if the hash is in consumed, burn the entire grant
//     family and return RotateResult{Theft: true}.
//  3. Look up in live tokens; if absent, return RotateResult{}.
//  4. If expired, delete and return RotateResult{}.
//  5. Move old hash to consumed, mint a fresh token, register it in tokens
//     and grants, return RotateResult{OK: true, NewToken: ..., Data: ...}.
func (s *RefreshTokenStore) Rotate(oldToken string, ttl time.Duration) RotateResult {
	result, mutated := s.rotate(oldToken, ttl)

	// Theft burns the grant family and an expired token tears it down, so
	// those write as surely as a successful rotation does. An unknown token
	// changes nothing, and must not write: anyone can post a made-up refresh
	// token to the token endpoint, and rewriting the whole file for each one
	// would turn that into an amplified write.
	if mutated {
		s.writeThrough()
	}

	return result
}

// rotate performs the rotation and reports whether it changed durable state.
func (s *RefreshTokenStore) rotate(oldToken string, ttl time.Duration) (RotateResult, bool) {
	oldHash := hashToken(oldToken)

	// Generate the candidate replacement outside the critical section so
	// crypto/rand.Read cannot block other callers of Validate/Rotate. The
	// allocation is wasted on the non-rotating paths (theft, unknown,
	// expired), but those are rare.
	newRaw := generateOpaqueToken()
	newHash := hashToken(newRaw)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Step 2: replay / theft check runs before anything else. On theft, the
	// grant family is burned.
	if grantID, isConsumed := s.consumed[oldHash]; isConsumed {
		// Naming the burned family lets the caller clear the user's consent
		// record and log who was affected, neither of which was possible when
		// this returned a zero result.
		return RotateResult{Theft: true, Data: s.revokeGrantLocked(grantID)}, true
	}

	// Step 3: live token lookup.
	entry, exists := s.tokens[oldHash]
	if !exists {
		return RotateResult{}, false
	}

	// Step 4: expiry check. Tear down the whole family so consumed and grants
	// don't accumulate orphaned entries past the token's natural lifetime.
	if time.Now().After(entry.expiresAt) {
		// The identity is deliberately dropped: an expired grant must not
		// clear the user's consent.
		s.revokeGrantLocked(entry.data.grantID)

		return RotateResult{}, true
	}

	// Step 5: rotate. Per-token expiry refreshes, but grantExpiresAt carries
	// forward unchanged so a long-lived refresher cannot extend the family
	// past its absolute lifetime.
	grantID := entry.data.grantID
	delete(s.tokens, oldHash)
	s.consumed[oldHash] = grantID

	newExpiry := time.Now().Add(ttl)
	if entry.grantExpiresAt.Before(newExpiry) {
		newExpiry = entry.grantExpiresAt
	}
	s.tokens[newHash] = refreshTokenEntry{
		data:           entry.data,
		expiresAt:      newExpiry,
		grantExpiresAt: entry.grantExpiresAt,
	}
	s.grants[grantID] = append(s.grants[grantID], newHash)
	if overflow := len(s.grants[grantID]) - maxGrantHistorySize; overflow > 0 {
		for _, evicted := range s.grants[grantID][:overflow] {
			delete(s.consumed, evicted)
		}
		s.grants[grantID] = s.grants[grantID][overflow:]
	}

	out := entry.data
	out.Groups = append([]string(nil), entry.data.Groups...)
	return RotateResult{
		NewToken: newRaw,
		Data:     out,
		OK:       true,
	}, true
}

// RevokeGrant revokes every token in the grant family that contains the given
// token, whether that token is currently live or already consumed. It returns
// the identity the family belonged to, so the caller can clear the matching
// consent record, and false when the token belongs to no known family.
func (s *RefreshTokenStore) RevokeGrant(token string) (RefreshTokenData, bool) {
	owner, revoked := s.revokeGrant(token)
	if revoked {
		s.writeThrough()
	}

	return owner, revoked
}

// revokeGrant revokes the family and reports whether one was found. A token
// belonging to no known family is a no-op, and must not write.
func (s *RefreshTokenStore) revokeGrant(token string) (RefreshTokenData, bool) {
	h := hashToken(token)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Determine grantID from either the live tokens map or the consumed map.
	grantID := ""
	if entry, exists := s.tokens[h]; exists {
		grantID = entry.data.grantID
	} else if id, exists := s.consumed[h]; exists {
		grantID = id
	}

	if grantID == "" {
		return RefreshTokenData{}, false
	}

	return s.revokeGrantLocked(grantID), true
}

// revokeGrantLocked removes every hash ever associated with grantID from both
// tokens and consumed, then drops the grant record. It returns the identity
// the family belonged to, read from its live token — a family has exactly one,
// unless it has already expired, in which case the zero value comes back.
//
// Whether revoking a family should also drop the user's consent record is the
// caller's decision, not this function's: an explicit revocation and a theft
// must clear it, and an expiry must not, because consent outliving the refresh
// token is the point of remembering it. Must be called with s.mu held.
func (s *RefreshTokenStore) revokeGrantLocked(grantID string) RefreshTokenData {
	var owner RefreshTokenData

	for _, h := range s.grants[grantID] {
		if entry, live := s.tokens[h]; live {
			owner = entry.data
		}

		delete(s.tokens, h)
		delete(s.consumed, h)
	}

	delete(s.grants, grantID)

	return owner
}
