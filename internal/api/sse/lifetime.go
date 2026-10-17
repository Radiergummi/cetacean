package sse

import (
	"context"
	"net/http"
	"sync"
)

// Lifetime ends long-lived requests when the server shuts down: Shutdown waits
// for every handler, and a stream returns only when its client leaves. Only a
// request bound to it ends early; an ordinary one still finishes its work.
type Lifetime struct {
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func NewLifetime() *Lifetime {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: End calls it

	return &Lifetime{ctx: ctx, cancel: cancel}
}

// End ends every bound request, for http.Server.RegisterOnShutdown.
func (l *Lifetime) End() {
	l.once.Do(l.cancel)
}

// Bind returns r with a context that also ends at End, and the release the
// caller defers. A nil Lifetime returns r as it is.
func (l *Lifetime) Bind(r *http.Request) (*http.Request, func()) {
	if l == nil {
		return r, func() {}
	}

	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(l.ctx, cancel)

	return r.WithContext(ctx), func() {
		stop()
		cancel()
	}
}
