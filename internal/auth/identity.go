package auth

import "context"

// Identity represents an authenticated user.
type Identity struct {
	Subject     string         `json:"subject"`
	DisplayName string         `json:"displayName"`
	Email       string         `json:"email,omitempty"`
	Groups      []string       `json:"groups,omitempty"`
	Provider    string         `json:"provider"`
	Raw         map[string]any `json:"raw,omitempty"`
}

// ProviderToken is the Provider of an identity the middleware built from a
// bearer token this deployment issued, rather than from the upstream provider.
// Declared here because both the issuer and the consumers of the distinction
// import this package, and neither imports the other.
const ProviderToken = "oauth"

// FromToken reports whether a bearer token carried this identity. What the
// caller may do can depend on it: a token is a credential left on a device,
// which a deployment may hold to less than the person it speaks for.
func (i *Identity) FromToken() bool {
	return i != nil && i.Provider == ProviderToken
}

type ctxKey struct{}

// ContextWithIdentity returns a new context with the given identity stored.
func ContextWithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// IdentityFromContext returns the identity from the context, or nil if none.
func IdentityFromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(ctxKey{}).(*Identity)
	return id
}
