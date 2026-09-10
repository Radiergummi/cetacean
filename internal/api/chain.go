package api

import (
	"net/http"
	"slices"
)

// Constructor wraps a handler in one layer of middleware.
type Constructor func(http.Handler) http.Handler

// Chain is an immutable, ordered list of middleware. The first Constructor is
// the outermost wrapper, so a chain reads in the order requests traverse it.
//
// Derived from justinas/alice, minus its Then(nil) fallback to
// http.DefaultServeMux: a nil handler here is a wiring bug, not a route.
type Chain struct {
	constructors []Constructor
}

// NewChain returns a Chain of the given constructors. The caller's slice is
// copied, so mutating it afterwards cannot change the chain.
func NewChain(constructors ...Constructor) Chain {
	return Chain{append(([]Constructor)(nil), constructors...)}
}

// Append returns a new Chain with the given constructors added at the end; the
// receiver is unchanged. The exact-capacity slice is load-bearing: with a plain
// append, two chains derived from one base can share a backing array and the
// second derivation overwrites the first one's middleware.
func (c Chain) Append(constructors ...Constructor) Chain {
	merged := make([]Constructor, 0, len(c.constructors)+len(constructors))
	merged = append(merged, c.constructors...)
	merged = append(merged, constructors...)

	return Chain{merged}
}

// Extend returns a new Chain with other's constructors appended to c's.
func (c Chain) Extend(other Chain) Chain {
	return c.Append(other.constructors...)
}

// Then wraps h in the chain, outermost constructor first.
func (c Chain) Then(h http.Handler) http.Handler {
	if h == nil {
		panic("api: Chain.Then called with a nil handler")
	}

	for _, constructor := range slices.Backward(c.constructors) {
		h = constructor(h)
	}

	return h
}

// ThenFunc is Then for a handler function.
func (c Chain) ThenFunc(fn http.HandlerFunc) http.Handler {
	return c.Then(fn)
}
