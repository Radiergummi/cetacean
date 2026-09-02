package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"hash"
	"slices"
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
