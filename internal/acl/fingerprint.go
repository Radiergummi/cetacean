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

// Fingerprint identifies what an identity is allowed to see: identities with
// equal fingerprints see the same rows everywhere and may share a cached
// response or validator. The policy generation is folded in, so a reload
// changes every fingerprint. A nil Evaluator or absent policy is one answer.
func (e *Evaluator) Fingerprint(id *auth.Identity) uint64 {
	if e == nil {
		return 0
	}
	p := e.policy.Load()
	if p == nil {
		return 0
	}

	// Summing is commutative, so the order of policy and provider grants does
	// not change the fingerprint. Walked inline rather than via collectGrants:
	// this runs on every conditional request, and collecting would allocate.
	var sum uint64
	for _, g := range p.Grants {
		if audienceMatches(g, id, e.userClaims) {
			sum += grantHash(g)
		}
	}
	if e.source != nil && id != nil {
		for _, g := range e.sourceGrants(id) {
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
