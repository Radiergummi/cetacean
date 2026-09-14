package acl

import (
	"encoding/binary"
	"hash/maphash"

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
//
// The grants are walked here rather than through collectGrants: this runs on
// every conditional request, and summing as it goes costs nothing, where
// gathering a slice first costs an allocation per call.
func (e *Evaluator) Fingerprint(id *auth.Identity) uint64 {
	if e == nil {
		return 0
	}
	p := e.policy.Load()
	if p == nil {
		return 0
	}

	// Grant order depends on policy file order and on what the provider
	// returned, neither of which changes what the identity may see. Summing is
	// commutative, so an identical set of grants fingerprints identically.
	var sum uint64
	for _, g := range p.Grants {
		if audienceMatches(g, id) {
			sum += grantHash(g)
		}
	}
	if e.source != nil && id != nil {
		for _, g := range e.source.GrantsFor(id) {
			sum += grantHash(g)
		}
	}

	return mixHash(sum, e.PolicyGeneration())
}

// grantHash reduces one grant to a value independent of the order of its
// resources and permissions. The two are summed apart and only then mixed, so
// a string cannot move between them unnoticed.
func grantHash(g Grant) uint64 {
	var resources, permissions uint64
	for _, r := range g.Resources {
		resources += maphash.String(fingerprintSeed, r)
	}
	for _, p := range g.Permissions {
		permissions += maphash.String(fingerprintSeed, p)
	}

	return mixHash(resources, permissions)
}

// mixHash combines two hashes non-linearly, which is what keeps the sum over
// grants from blurring their boundaries: were this an add or an xor, two
// policies that swapped one grant's permissions for another's would agree.
func mixHash(a, b uint64) uint64 {
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[0:], a)
	binary.LittleEndian.PutUint64(buf[8:], b)

	return maphash.Bytes(fingerprintSeed, buf[:])
}
