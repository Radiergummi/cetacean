package cluster

import (
	"context"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// Attributed marks the writes made under ctx as the caller's, arriving over
// transport, so the history names who made them. Without an identity there
// is no one to name, and ctx is returned as it is.
func Attributed(ctx context.Context, transport cache.Transport) context.Context {
	id := auth.IdentityFromContext(ctx)
	if id == nil {
		return ctx
	}

	return cache.WithActor(ctx, cache.Actor{
		Subject:   id.Subject,
		Name:      id.DisplayName,
		Provider:  id.Provider,
		Client:    id.ClientID,
		Transport: transport,
	})
}
