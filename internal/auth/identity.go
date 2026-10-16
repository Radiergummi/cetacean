package auth

import (
	"context"
	"time"
)

// Identity represents an authenticated user.
type Identity struct {
	Subject     string         `json:"subject"`
	DisplayName string         `json:"displayName"`
	Email       string         `json:"email,omitempty"`
	Groups      []string       `json:"groups,omitempty"`
	Provider    string         `json:"provider"`
	Raw         map[string]any `json:"raw,omitempty"`

	// ExpiresAt is when the credential behind the identity lapses, zero when
	// nothing bounds it here. Only a token this deployment issued sets it.
	ExpiresAt time.Time `json:"-"`
}

// ProviderToken is the Provider of an identity the middleware built from a
// bearer token this deployment issued, rather than from the upstream provider.
// Declared here because both the issuer and the consumers of the distinction
// import this package, and neither imports the other.
const ProviderToken = "oauth"

// RawTokenGrants is the Raw key holding the provider grants a token carries,
// as the JSON array the issuer was handed at consent.
const RawTokenGrants = "tokenGrants"

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

// ContextUntilExpiry stores id in ctx and ends the context when id's credential
// lapses, so a stream it opened cannot outlive the token. The caller cancels.
func ContextUntilExpiry(ctx context.Context, id *Identity) (context.Context, context.CancelFunc) {
	ctx = ContextWithIdentity(ctx, id)
	if id == nil || id.ExpiresAt.IsZero() {
		return ctx, func() {}
	}

	return context.WithDeadline(ctx, id.ExpiresAt)
}

// IdentityFromContext returns the identity from the context, or nil if none.
func IdentityFromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(ctxKey{}).(*Identity)
	return id
}
