package contract

import (
	"slices"
	"strings"
	"testing"
)

func TestPreconditionedRoutesFindsBothWiringForms(t *testing.T) {
	routes, err := PreconditionedRoutes()
	if err != nil {
		t.Fatalf("PreconditionedRoutes: %v", err)
	}

	for _, want := range []Route{
		// Appended inline in the registration.
		{Method: "PATCH", Pattern: "/services/{id}/env"},
		{Method: "DELETE", Pattern: "/services/{id}"},

		// The two healthcheck methods share one chain variable, built once
		// and registered twice. Only the carrier pass sees these; without it
		// the completeness check below would report the call site as
		// unreachable rather than silently dropping them.
		{Method: "PUT", Pattern: "/services/{id}/healthcheck"},
		{Method: "PATCH", Pattern: "/services/{id}/healthcheck"},
	} {
		if !slices.Contains(routes, want) {
			t.Errorf("PreconditionedRoutes is missing %s", want)
		}
	}
}

// TestPreconditionedRoutesAreRegisteredRoutes pins the two inventories to one
// another: a precondition on a pattern the route inventory has never heard of
// means one of the two parsers is reading the file wrong.
func TestPreconditionedRoutesAreRegisteredRoutes(t *testing.T) {
	preconditioned, err := PreconditionedRoutes()
	if err != nil {
		t.Fatalf("PreconditionedRoutes: %v", err)
	}

	routes, err := Routes()
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}

	if len(preconditioned) >= len(routes) {
		t.Errorf(
			"PreconditionedRoutes reports %d of %d routes; preconditions are a "+
				"minority of the surface, so the parser is over-matching",
			len(preconditioned), len(routes),
		)
	}

	for _, p := range preconditioned {
		if !slices.Contains(routes, p) {
			t.Errorf("PreconditionedRoutes reports %s, which is not a registered route", p)
		}
	}

	if !slices.IsSortedFunc(preconditioned, func(a, b Route) int {
		return strings.Compare(a.String(), b.String())
	}) {
		t.Error("PreconditionedRoutes is not sorted; the excused-list diff would be unstable")
	}
}

// TestPreconditionedRoutesRejectsAnUnreachablePrecond is the guard that makes
// the inventory trustworthy: a precondition wired in a form the parser cannot
// follow must be an error, never a route quietly missing from the gate.
func TestPreconditionedRoutesRejectsAnUnreachablePrecond(t *testing.T) {
	_, err := parsePreconditionedFromSource("unreachable.go", `
package api

func register(mux *http.ServeMux, h *Handlers) {
	chain := base.Append(h.precond(h.serviceRepresentation))
	_ = chain

	mux.Handle("DELETE /services/{id}", base.ThenFunc(h.HandleRemoveService))
}
`)
	if err == nil {
		t.Fatal("parsePreconditionedFromSource accepted a precond reaching no registration")
	}

	if !strings.Contains(err.Error(), "reaches no registration") {
		t.Errorf("error does not name the unreachable call site: %v", err)
	}
}

// TestPreconditionedRoutesFollowsAChainOfChains covers the fixed point: a chain
// variable built from another chain variable still carries the middleware.
func TestPreconditionedRoutesFollowsAChainOfChains(t *testing.T) {
	routes, err := parsePreconditionedFromSource("chained.go", `
package api

func register(mux *http.ServeMux, h *Handlers) {
	inner := base.Append(h.precond(h.serviceRepresentation))
	outer := inner.Append(h.requireWriteACL("service"))

	mux.Handle("DELETE /services/{id}", outer.ThenFunc(h.HandleRemoveService))
}
`)
	if err != nil {
		t.Fatalf("parsePreconditionedFromSource: %v", err)
	}

	want := Route{Method: "DELETE", Pattern: "/services/{id}"}
	if !slices.Contains(routes, want) {
		t.Errorf("a chain built from a preconditioned chain lost the precondition: %v", routes)
	}
}
