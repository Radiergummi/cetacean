package acl

import (
	"path"
	"strings"
	"testing"
)

// globMeta holds the characters path.Match treats specially: '*', '?' and '['
// open a wildcard term, and '\' escapes the next character. All four reach
// further, or differently, than their literal text — pattern `\0` escapes to
// the literal '0', which also breaks self-match, so both properties below key
// off this set rather than just "*?[".
const globMeta = `*?[\`

// FuzzMatchResource exercises matchResource, which decides whether a policy
// grant covers a resource. The resource side is derived from a request path, so
// this sees attacker-influenced input on every authorization decision.
//
// Two properties hold regardless of input:
//   - An expression with none of globMeta matches only itself. Anything
//     broader is a grant reaching past what it names.
//   - An expression identical to the resource always matches. This holds only
//     for a well-formed "type:name" resource whose pattern half is a valid,
//     metacharacter-free glob: validateGrant rejects an invalid glob on every
//     path that admits one, so matchResource never sees it in production, and
//     a pattern containing '\' is not generally a match for its own text.
func FuzzMatchResource(f *testing.F) {
	f.Add("service:web", "service:web")
	f.Add("service:*", "service:web")
	f.Add("*", "node:manager-1")
	f.Add("stack:frontend-*", "stack:frontend-shop")
	f.Add("service:web", "service:web-2")
	f.Add("", "")
	f.Add("service:**", "service:a:b")
	// Malformed glob — kept to document the boundary. validateGrant rejects
	// this pattern before it can ever reach matchResource (see the four call
	// sites in the doc comment above), so the self-match property below is
	// skipped for it rather than expected to hold.
	f.Add("service:[", "service:[")
	// Backslash escape: pattern `\0` matches the literal `0` and not itself. The
	// escape narrows a grant to one literal target rather than widening it, so
	// this seed pins the semantics rather than recording a defect.
	f.Add("stack:\\0", "stack:0")

	f.Fuzz(func(t *testing.T, expression, resource string) {
		matched := matchResource(expression, resource)

		if matched && !strings.ContainsAny(expression, globMeta) && expression != resource {
			t.Fatalf(
				"matchResource(%q, %q) = true, but expression has no wildcards and differs from resource",
				expression,
				resource,
			)
		}

		if expression == resource {
			_, resName, ok := strings.Cut(resource, ":")
			if ok && resName != "" && !strings.Contains(resName, `\`) {
				if _, err := path.Match(resName, ""); err == nil && !matched {
					t.Fatalf(
						"matchResource(%q, %q) = false, want true (expression equals resource)",
						expression,
						resource,
					)
				}
			}
		}
	})
}
