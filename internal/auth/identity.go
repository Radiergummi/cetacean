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
