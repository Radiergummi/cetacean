package acl

import (
	"hash/maphash"
	"slices"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
)

// fingerprintSeed is process-local, so a fingerprint is comparable only within
// this process. That is all it is for: deciding whether two requests in flight
// here may share a rendered response.
var fingerprintSeed = maphash.MakeSeed()

// Fingerprint identifies what an identity is allowed to see. Two identities
// with the same fingerprint see the same rows from every ACL-filtered endpoint,
// and may therefore share a cached response or a validator; two with different
// fingerprints must never be given each other's.
//
// The policy generation is folded in, so a reload changes every fingerprint
// without any cache mutation — a grant removed from the file has to invalidate
// what the identity it covered was already holding.
//
// A nil Evaluator or absent policy filters nothing, which is one shared answer
// rather than a per-identity one, and fingerprints accordingly.
func (e *Evaluator) Fingerprint(id *auth.Identity) uint64 {
	if e == nil {
		return 0
	}
	p := e.policy.Load()
	if p == nil {
		return 0
	}

	grants := e.collectGrants(id, p)

	// Grant order depends on policy file order and on what the provider
	// returned, neither of which changes what the identity may see. Normalise
	// so an identical set of grants fingerprints identically.
	lines := make([]string, 0, len(grants))
	for _, g := range grants {
		resources := slices.Clone(g.Resources)
		permissions := slices.Clone(g.Permissions)
		slices.Sort(resources)
		slices.Sort(permissions)
		lines = append(lines,
			strings.Join(resources, ",")+"\x00"+strings.Join(permissions, ","))
	}
	slices.Sort(lines)

	var h maphash.Hash
	h.SetSeed(fingerprintSeed)

	// The generation goes in as bytes rather than as text so it cannot collide
	// with a grant line that happens to spell the same digits.
	var gen [8]byte
	v := e.PolicyGeneration()
	for i := range gen {
		gen[i] = byte(v >> (8 * i))
	}
	_, _ = h.Write(gen[:])

	for _, l := range lines {
		_, _ = h.WriteString(l)
		// Without a separator two different splits of the same characters
		// hash alike.
		_, _ = h.Write([]byte{0x1e})
	}

	return h.Sum64()
}
