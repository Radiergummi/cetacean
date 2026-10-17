package sse

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLifetimeEndsABoundRequestAtShutdown(t *testing.T) {
	l := NewLifetime()
	r, release := l.Bind(httptest.NewRequest("GET", "/", nil))
	defer release()

	l.End()

	select {
	case <-r.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("a bound request outlived End")
	}
}

func TestLifetimeBindKeepsTheRequestsOwnEnd(t *testing.T) {
	l := NewLifetime()
	ctx, cancel := context.WithCancel(context.Background())
	r, release := l.Bind(httptest.NewRequest("GET", "/", nil).WithContext(ctx))
	defer release()

	cancel()

	select {
	case <-r.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("a bound request outlived its client")
	}
}

func TestLifetimeLeavesUnboundRequestsAlone(t *testing.T) {
	l := NewLifetime()
	r := httptest.NewRequest("GET", "/", nil)

	l.End()
	l.End()

	if r.Context().Err() != nil {
		t.Fatal("End reached a request that was never bound")
	}

	var unset *Lifetime
	bound, release := unset.Bind(r)
	defer release()

	if bound != r {
		t.Fatal("a nil Lifetime should hand the request back unchanged")
	}
}
