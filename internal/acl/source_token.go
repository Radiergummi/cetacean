package acl

import (
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/auth"
)

// sourceGrants is what the provider gives id, derived once per identity and
// evaluator: a source parses a header or claim on every call.
func (e *Evaluator) sourceGrants(id *auth.Identity) []Grant {
	grants, _ := id.Derived(sourceGrantsKey{e}, func() any {
		return e.deriveSourceGrants(id)
	}).([]Grant)

	return grants
}

type sourceGrantsKey struct{ evaluator *Evaluator }

// deriveSourceGrants is sourceGrants uncached. A token this deployment issued
// carries none of the headers or claims a source reads, so it answers with the
// grants SnapshotGrants recorded when the user consented instead.
func (e *Evaluator) deriveSourceGrants(id *auth.Identity) []Grant {
	if !id.FromToken() {
		return e.source.GrantsFor(id)
	}

	raw, _ := id.Raw[auth.RawTokenGrants].([]any)

	return extractGrantsFromRaw(raw)
}

// SnapshotGrants serializes the grants the provider gives id now, for a token
// minted on its behalf to carry. Nil when there is no source or nothing to carry.
func (e *Evaluator) SnapshotGrants(id *auth.Identity) []byte {
	if e == nil || e.source == nil || id == nil || id.FromToken() {
		return nil
	}

	grants := e.sourceGrants(id)
	if len(grants) == 0 {
		return nil
	}

	out, err := json.Marshal(grants)
	if err != nil {
		return nil
	}

	return out
}
