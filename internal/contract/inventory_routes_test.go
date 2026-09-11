package contract

import (
	"slices"
	"strings"
	"testing"
)

func TestRoutesFindsTheRegisteredSurface(t *testing.T) {
	routes, err := Routes()
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}

	// The router registered 135 patterns when this was written. An exact
	// number would be a second statement of the same fact and would fail on
	// every legitimate addition; a floor catches a parser that silently
	// stopped finding things.
	if len(routes) < 100 {
		t.Errorf(
			"Routes found %d registrations; the router holds well over 100. "+
				"The parser is probably missing a registration form.",
			len(routes),
		)
	}

	for _, want := range []Route{
		{Method: "GET", Pattern: "/-/health"},
		{Method: "GET", Pattern: "/nodes"},
		{Method: "GET", Pattern: "/services/{id}"},
		{Method: "PUT", Pattern: "/services/{id}/scale"},
	} {
		if !slices.Contains(routes, want) {
			t.Errorf("Routes is missing %s", want)
		}
	}
}

func TestRoutesAreDeduplicatedAndSorted(t *testing.T) {
	routes, err := Routes()
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}

	seen := make(map[string]bool, len(routes))

	for _, r := range routes {
		if seen[r.String()] {
			t.Errorf("Routes reports %s more than once", r)
		}

		seen[r.String()] = true
	}

	if !slices.IsSortedFunc(routes, func(a, b Route) int {
		return strings.Compare(a.String(), b.String())
	}) {
		t.Error("Routes is not sorted; the excused-list diff would be unstable")
	}
}

func TestRoutesScopesRangeResolutionToTheEnclosingLoop(t *testing.T) {
	// Two range loops share the loop-variable name "removed". The real
	// registration sits in the first loop; the second, unrelated loop reuses
	// the name with different elements. Resolution must use the loop the
	// call is lexically inside, not whichever same-named loop happens to be
	// visited last.
	source := `package api

func NewRouter() {
	for _, removed := range []string{"/right/a", "/right/b"} {
		mux.HandleFunc("GET "+removed, nil)
	}

	for _, removed := range []string{"/wrong/a", "/wrong/b"} {
		_ = removed
	}
}`

	routes, err := parseRoutesFromSource("synthetic.go", source)
	if err != nil {
		t.Fatalf("parseRoutesFromSource: %v", err)
	}

	want := []Route{
		{Method: "GET", Pattern: "/right/a"},
		{Method: "GET", Pattern: "/right/b"},
	}

	if !slices.Equal(routes, want) {
		t.Errorf(
			"Routes = %v, want %v; resolution must use the call's own "+
				"enclosing loop, not a same-named loop elsewhere in the file",
			routes, want,
		)
	}
}

func TestRoutesRejectsANonLiteralPattern(t *testing.T) {
	source := `package api

func NewRouter() {
	prefix := "/x"
	mux.HandleFunc(prefix+"/nodes", nil)
}`

	if _, err := parseRoutesFromSource("synthetic.go", source); err == nil {
		t.Fatal(
			"parseRoutes accepted a computed pattern. Such a route would be " +
				"invisible to the inventory, so it must be an error, not a skip.",
		)
	}
}
