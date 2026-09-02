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

// consentFingerprint hashes what the consent page showed the user: the
// client's name, and the exact set of URIs it may receive a code at.
//
// A CIMD client controls its own metadata document and may change it at any
// time. Binding a remembered approval to this hash means a rename or a new
// redirect_uri re-prompts, so an approval can never be inherited by a client
// the user would not recognise, or redirect somewhere they never saw.
//
// The URIs are sorted because array order in a client-controlled document
// carries no meaning; re-prompting on a reordering would be noise. Fields are
// length-prefixed so no two distinct tuples can produce the same byte stream,
// including when a field itself contains the delimiter.
func consentFingerprint(meta *ClientMetadata) string {
	uris := slices.Clone(meta.RedirectURIs)
	slices.Sort(uris)

	h := sha256.New()
	hashField(h, consentFingerprintDomain)
	hashField(h, meta.ClientName)

	for _, uri := range uris {
		hashField(h, uri)
	}

	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// ConsentRecord is one remembered approval: this user approved this client,
// as it looked at the time, for this MCP endpoint.
type ConsentRecord struct {
	Subject     string    `json:"subject"`
	ClientID    string    `json:"clientId"`
	Resource    string    `json:"resource"`
	Fingerprint string    `json:"fingerprint"`
	GrantedAt   time.Time `json:"grantedAt"`
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
	mu      sync.Mutex
	records map[string]ConsentRecord

	// onChange is called after every mutation that changed state, always with
	// mu released. Nil when the store is memory-only.
	onChange func()
}

// NewConsentStore returns a ready-to-use ConsentStore.
func NewConsentStore() *ConsentStore {
	return &ConsentStore{
		records: make(map[string]ConsentRecord),
	}
}

// SetOnChange installs the callback fired after every mutation. Call it during
// setup, before the store serves any request: it is not guarded by the mutex,
// because a hook swapped mid-flight would race the mutations it records.
func (s *ConsentStore) SetOnChange(fn func()) {
	s.onChange = fn
}

// writeThrough notifies the owner of the durable state that it changed. It
// must be called with mu released, since the owner takes a snapshot, which
// acquires it.
func (s *ConsentStore) writeThrough() {
	if s.onChange == nil {
		return
	}

	s.onChange()
}

// consentKeyDomain separates this hash from every other use of SHA-256 in the
// package, so a value from one can never be mistaken for the other.
const consentKeyDomain = "cetacean-consent-key-v1"

// consentKey identifies the approval, without the fingerprint: the fingerprint
// lives in the value, so a client whose metadata changed replaces its stale
// record rather than accumulating a second one alongside it.
//
// A subject can originate from an OIDC token claim, and JSON can encode any
// byte including NUL, so a plain separator-joined string cannot rule out one
// triple's bytes spelling another's — e.g. ("a\x00b", "c", "d") and
// ("a", "b\x00c", "d") would join identically. Each field is instead
// length-prefixed via hashField before being folded into a SHA-256, so the
// composition is unambiguous regardless of what bytes a field contains.
func consentKey(subject, clientID, resource string) string {
	h := sha256.New()
	hashField(h, consentKeyDomain)
	hashField(h, subject)
	hashField(h, clientID)
	hashField(h, resource)

	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// Allows reports whether this exact approval was remembered — same user, same
// client, same MCP endpoint, and the same client metadata they were shown.
func (s *ConsentStore) Allows(subject, clientID, resource, fingerprint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, exists := s.records[consentKey(subject, clientID, resource)]

	return exists && record.Fingerprint == fingerprint
}

// Remember records an approval, replacing any earlier one for the same user,
// client and resource.
func (s *ConsentStore) Remember(subject, clientID, resource, fingerprint string) {
	key := consentKey(subject, clientID, resource)

	s.mu.Lock()
	existing, exists := s.records[key]
	unchanged := exists && existing.Fingerprint == fingerprint
	if !unchanged {
		s.records[key] = ConsentRecord{
			Subject:     subject,
			ClientID:    clientID,
			Resource:    resource,
			Fingerprint: fingerprint,
			GrantedAt:   time.Now(),
		}
	}
	s.mu.Unlock()

	// Re-approving an unchanged client changes nothing durable, and rewriting
	// the whole file for it would be work for no reason.
	if unchanged {
		return
	}

	s.writeThrough()
}

// Forget drops an approval. Called when a grant is revoked or burned for
// theft, so revocation cannot degrade into "grant one more silent
// re-authorization".
func (s *ConsentStore) Forget(subject, clientID, resource string) {
	key := consentKey(subject, clientID, resource)

	s.mu.Lock()
	_, existed := s.records[key]
	delete(s.records, key)
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

// Restore replaces the store's state from a snapshot.
func (s *ConsentStore) Restore(records []ConsentRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.records = make(map[string]ConsentRecord, len(records))
	for _, record := range records {
		s.records[consentKey(record.Subject, record.ClientID, record.Resource)] = record
	}
}
