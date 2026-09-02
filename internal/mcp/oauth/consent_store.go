package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"slices"
)

// consentFingerprintDomain separates this hash from every other use of
// SHA-256 in the package, so a value from one can never be mistaken for the
// other, and lets a future change to what is fingerprinted invalidate old
// records rather than silently reinterpret them.
const consentFingerprintDomain = "cetacean-consent-v1"

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
// NUL-separated so ("ab", "c") cannot hash the same as ("a", "bc").
func consentFingerprint(meta *ClientMetadata) string {
	uris := slices.Clone(meta.RedirectURIs)
	slices.Sort(uris)

	h := sha256.New()
	h.Write([]byte(consentFingerprintDomain))
	h.Write([]byte{0})
	h.Write([]byte(meta.ClientName))

	for _, uri := range uris {
		h.Write([]byte{0})
		h.Write([]byte(uri))
	}

	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
