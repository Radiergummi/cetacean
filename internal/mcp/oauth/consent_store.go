package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"hash"
	"slices"
	"strings"
	"sync"
	"time"
)

// consentFingerprintDomain separates this hash from every other use of
// SHA-256 in the package, so a value from one can never be mistaken for the
// other, and lets a future change to what is fingerprinted invalidate old
// records rather than silently reinterpret them.
const consentFingerprintDomain = "cetacean-consent-v1"

// consentKeyDomain separates this hash from every other use of SHA-256 in the
// package, so a value from one can never be mistaken for the other.
const consentKeyDomain = "cetacean-consent-key-v1"

// hashField writes a length-prefixed field, so the hash is an unambiguous
// encoding of its inputs. A separator byte is not enough: client_name comes
// from a JSON document the client controls, and JSON can encode any byte
// including the separator, letting one field's content spill into the next
// and two different clients share a fingerprint.
func hashField(h hash.Hash, field string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(field)))
	h.Write(length[:])
	h.Write([]byte(field))
}

// hashFields folds a domain separator and its fields into one SHA-256. Every
// hashed value in this package goes through here, so the two disciplines the
// comments above defend — domain separation and length-prefixing — are
// implemented once rather than restated at each new hash.
func hashFields(domain string, fields ...string) string {
	h := sha256.New()
	hashField(h, domain)

	for _, field := range fields {
		hashField(h, field)
	}

	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// consentFingerprint hashes what the consent page showed the user: the
// client's name, and the exact set of URIs it may receive a code at.
//
// A CIMD client controls its own metadata document and may change it at any
// time. Binding a remembered approval to this hash means a rename or a new
// redirect_uri re-prompts, so an approval can never be inherited by a client
// the user would not recognise, or redirect somewhere they never saw.
//
// The URIs are sorted because array order in a client-controlled document
// carries no meaning; re-prompting on a reordering would be noise.
func consentFingerprint(meta *ClientMetadata) string {
	uris := slices.Clone(meta.RedirectURIs)
	slices.Sort(uris)

	return hashFields(consentFingerprintDomain, append([]string{meta.ClientName}, uris...)...)
}

// ConsentKey identifies one approval: this user, this client, this MCP
// endpoint. The three are all free-form strings, so they travel as a named
// struct — passed positionally, a transposed pair compiles and silently keys a
// different record.
type ConsentKey struct {
	Subject  string `json:"subject"`
	ClientID string `json:"clientId"`
	Resource string `json:"resource"`
}

// hash identifies the approval, without the fingerprint: the fingerprint lives
// in the value, so a client whose metadata changed replaces its stale record
// rather than accumulating a second one alongside it.
//
// A subject can originate from an OIDC token claim, and JSON can encode any
// byte including NUL, so a plain separator-joined string cannot rule out one
// triple's bytes spelling another's — e.g. ("a\x00b", "c", "d") and
// ("a", "b\x00c", "d") would join identically. hashFields length-prefixes each
// field, so the composition is unambiguous whatever bytes a field contains.
func (k ConsentKey) hash() string {
	return hashFields(consentKeyDomain, k.Subject, k.ClientID, k.Resource)
}

// ConsentKey projects the identity a refresh token was issued to onto the
// approval it was granted under, so the token-to-consent mapping is written
// once rather than spelled out at each teardown site.
func (d RefreshTokenData) ConsentKey() ConsentKey {
	return ConsentKey{Subject: d.Subject, ClientID: d.ClientID, Resource: d.Resource}
}

// ConsentRecord is one remembered approval: this user approved this client,
// as it looked at the time, for this MCP endpoint.
type ConsentRecord struct {
	ConsentKey

	Fingerprint string `json:"fingerprint"`

	// GrantedAt is not read by the server. It is recorded so an operator
	// reading the state file can tell when an approval was given, and when a
	// stale-looking one is safe to delete by hand.
	GrantedAt time.Time `json:"grantedAt"`
}

// ConsentStore remembers approvals so an already-approved client does not
// re-prompt on every authorization request.
//
// Unlike the refresh token store, whose file holds only hashes and is
// therefore a confidentiality concern, a consent record is a capability:
// anyone able to write it can pre-approve a client and complete an
// authorization with no human in the loop. The file is mode 0600 and the
// caveat is the usual one — an attacker with host access has already won —
// but the property being protected is integrity, not secrecy.
type ConsentStore struct {
	changeNotifier

	// ttl bounds how long a record keeps skipping the consent screen.
	//
	// The store needs a lifetime of its own because it cannot borrow the token
	// store's. An approval is only reachable for revocation by presenting a
	// token from its grant family, and a family is torn down once its token
	// expires — so an unbounded record outlives the only handle anyone had on
	// it and keeps authorizing silently with no way left to withdraw it. A
	// max-age is what makes "remembered" a lease rather than a one-way door.
	//
	// Zero or negative disables remembering: nothing is recorded and nothing
	// is honoured, so every authorization reaches a human.
	ttl time.Duration

	mu      sync.Mutex
	records map[string]ConsentRecord
}

// NewConsentStore returns a ready-to-use ConsentStore whose approvals lapse
// after ttl. A ttl of zero or less disables remembering altogether.
func NewConsentStore(ttl time.Duration) *ConsentStore {
	return &ConsentStore{
		ttl:     ttl,
		records: make(map[string]ConsentRecord),
	}
}

// Enabled reports whether approvals are remembered at all.
func (s *ConsentStore) Enabled() bool {
	return s.ttl > 0
}

// TTL is how long a fresh approval will be honoured. The consent page reads it
// from here rather than from config, so what the page promises and what the
// store enforces cannot drift.
func (s *ConsentStore) TTL() time.Duration {
	return s.ttl
}

// expired reports whether a record granted at grantedAt has outlived the
// store's lease. Evaluated against a caller-supplied now so a single sweep
// judges every record against one instant.
func (s *ConsentStore) expired(grantedAt time.Time, now time.Time) bool {
	return now.Sub(grantedAt) >= s.ttl
}

// Allows reports whether this exact approval was remembered — same user, same
// client, same MCP endpoint, and the same client metadata they were shown.
// A lapsed record is reported as not allowed but deliberately left in place:
// this is the read path, and deleting here would turn every authorization into
// a potential file write. Restore drops them at the next start.
func (s *ConsentStore) Allows(key ConsentKey, fingerprint string) bool {
	if !s.Enabled() {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record, exists := s.records[key.hash()]

	return exists &&
		record.Fingerprint == fingerprint &&
		!s.expired(record.GrantedAt, time.Now())
}

// Remember records an approval, replacing any earlier one for the same user,
// client and resource.
func (s *ConsentStore) Remember(key ConsentKey, fingerprint string) {
	// Recording an approval that Allows would never honour costs a file write
	// and leaves a capability on disk for no benefit.
	if !s.Enabled() {
		return
	}

	hash := key.hash()

	s.mu.Lock()

	// Re-approving an unchanged client changes nothing durable, and rewriting
	// the whole file for it would be work for no reason. A record close enough
	// to lapsing is rewritten anyway, since re-approval is what renews the
	// lease.
	existing, exists := s.records[hash]
	if exists && existing.Fingerprint == fingerprint && !s.expired(existing.GrantedAt, time.Now()) {
		s.mu.Unlock()

		return
	}

	s.records[hash] = ConsentRecord{
		ConsentKey:  key,
		Fingerprint: fingerprint,
		GrantedAt:   time.Now(),
	}
	s.mu.Unlock()

	s.writeThrough()
}

// Forget drops an approval. Called when a grant is revoked or burned for
// theft, so revocation cannot degrade into "grant one more silent
// re-authorization".
func (s *ConsentStore) Forget(key ConsentKey) {
	hash := key.hash()

	s.mu.Lock()
	_, existed := s.records[hash]
	delete(s.records, hash)
	s.mu.Unlock()

	if !existed {
		return
	}

	s.writeThrough()
}

// Snapshot returns the records in a stable order. Map iteration is random, and
// an unsorted snapshot would rewrite the file with reordered records on every
// unrelated change.
func (s *ConsentStore) Snapshot() []ConsentRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	records := make([]ConsentRecord, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}

	slices.SortFunc(records, func(a, b ConsentRecord) int {
		if c := strings.Compare(a.Subject, b.Subject); c != 0 {
			return c
		}
		if c := strings.Compare(a.ClientID, b.ClientID); c != 0 {
			return c
		}

		return strings.Compare(a.Resource, b.Resource)
	})

	return records
}

// Restore replaces the store's state from a snapshot, dropping approvals that
// lapsed while the process was down. This is the store's only pruning point:
// Allows refuses a lapsed record but leaves it alone, so without this the file
// would accumulate records that can never authorize anything again.
func (s *ConsentStore) Restore(records []ConsentRecord) {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.records = make(map[string]ConsentRecord, len(records))
	if !s.Enabled() {
		return
	}

	for _, record := range records {
		if s.expired(record.GrantedAt, now) {
			continue
		}

		s.records[record.hash()] = record
	}
}
