package acl

import (
	"path"
	"strings"
	"testing"
)

// globMeta holds the characters path.Match treats specially: '*', '?' and
// '[' open a wildcard term, and '\' escapes the next character (per the
// package doc for path.Match). All four reach further, or differently, than
// their literal text — a fuzz-discovered case is
// matchResource("stack:\0", "stack:0") == true, because pattern `\0` escapes
// to the literal character '0'; the same escaping also breaks self-match for
// an identical pattern (path.Match(`\0`, `\0`) is false, with no syntax
// error), which is why both properties below key off this set rather than
// just "*?[".
const globMeta = `*?[\`

// FuzzMatchResource exercises matchResource, which decides whether a policy
// grant covers a resource. The expression side comes from a policy file, but
// the resource side is derived from a request path, so this function sees
// attacker-influenced input on every authorization decision.
//
// Two properties hold regardless of input:
//   - An expression with none of globMeta matches only itself. Anything
//     broader means a grant reaching past what it names — privilege
//     escalation, not a matching quirk. Skipped when the expression carries
//     one of those characters, since each is meant to reach, or resolve to,
//     something other than its own literal text.
//   - An expression identical to the resource always matches, otherwise a
//     grant fails to cover the thing it names. This only holds for a
//     well-formed resource (has a "type:name" shape with a non-empty name —
//     matchResource itself never matches an empty name or a resource with no
//     colon) whose pattern half is also a *valid*, metacharacter-free glob:
//     an invalid glob (e.g. an unclosed "[") cannot reach matchResource in
//     production — validateGrant (internal/acl/policy.go:72) calls
//     path.Match(parts[1], "") on every grant resource and rejects the grant
//     if that errors, and this runs on every path that admits a grant —
//     acl.Validate at startup for both the inline-policy and policy-file
//     cases (main.go:330, main.go:348), again on hot reload
//     (internal/acl/reload.go:115), and inline in the provider-sourced path
//     that has no separate file to validate (internal/acl/source.go:48,
//     `validateGrant(g) == nil` gates admission) — so that case is excluded
//     rather than reported as a bug in matchResource, since matchResource
//     never sees one for real. A pattern containing '\' is excluded for a
//     different reason: it reaches production validation just fine (no
//     syntax error), but escaping means the pattern's own source text is not
//     generally a match for itself.
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
	// Backslash escape. path.Match treats `\` as an escape, so the pattern
	// `\0` matches the literal `0` and not itself. Found by the fuzzer after
	// 1.96M executions, when globMeta still omitted `\`. Correct behaviour —
	// the escape narrows a grant to one literal target rather than widening
	// it — so this seed pins the semantics rather than recording a defect.
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
