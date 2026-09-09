package api

import "net/http"

// Constructor wraps a handler in one layer of middleware.
type Constructor func(http.Handler) http.Handler

// Chain is an immutable, ordered list of middleware. Order is declaration
// order: the first Constructor is the outermost wrapper and runs first, so a
// chain reads top-to-bottom in the order requests actually traverse it.
//
// The type exists because the reassignment style it replaced read in reverse,
// which is how the Vary clobber between cors and negotiate went unnoticed.
//
// Derived from justinas/alice, vendored rather than imported: the package is
// small enough that the ideas travel better than a dependency, and this
// project ships its SBOM as a product feature. The one behaviour deliberately
// not carried over is Then(nil) falling back to http.DefaultServeMux — this
// codebase never touches the package-global mux, and resolving a nil handler
// to it turns a wiring bug into a routing mystery.
type Chain struct {
	constructors []Constructor
}

// NewChain returns a Chain of the given constructors. The caller's slice is
// copied, so mutating it afterwards cannot change the chain.
func NewChain(constructors ...Constructor) Chain {
	return Chain{append(([]Constructor)(nil), constructors...)}
}

// Append returns a new Chain with the given constructors added at the end.
// The receiver is unchanged.
//
// The fresh exact-capacity slice is load-bearing, not defensive style. With a
// plain append, two chains derived from one base can share a backing array
// with spare capacity, and the second derivation silently overwrites the
// first one's middleware.
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

	for i := len(c.constructors) - 1; i >= 0; i-- {
		h = c.constructors[i](h)
	}

	return h
}

// ThenFunc is Then for a handler function. Route handlers in this package are
// method values (func(http.ResponseWriter, *http.Request)), so this is the
// form nearly every callsite uses.
func (c Chain) ThenFunc(fn http.HandlerFunc) http.Handler {
	return c.Then(fn)
}
