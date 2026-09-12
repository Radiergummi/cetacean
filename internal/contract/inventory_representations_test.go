package contract

import (
	"slices"
	"testing"
)

func TestRepresentationsReadsBothNegotiationForms(t *testing.T) {
	found, err := Representations()
	if err != nil {
		t.Fatalf("Representations: %v", err)
	}

	for _, tc := range []struct {
		route string
		want  []Representation
	}{
		// The dispatch-helper form, in its three shapes: no feeds, feeds from
		// a builder, and SSE.
		{"GET /cluster", []Representation{RepresentationJSON, RepresentationHTML}},
		{"GET /services", []Representation{
			RepresentationJSON, RepresentationHTML, RepresentationSSE,
			RepresentationAtom, RepresentationJSONFeed, RepresentationCSV,
		}},

		// A feedHandlers composite literal naming its fields.
		{"GET /history", []Representation{
			RepresentationJSON, RepresentationHTML,
			RepresentationAtom, RepresentationJSONFeed, RepresentationCSV,
		}},

		// The hand-written switch, which no helper call describes.
		{"GET /topology", []Representation{
			RepresentationJSON, RepresentationHTML,
			RepresentationJGF, RepresentationGraphML, RepresentationDOT,
		}},
		{"GET /events", []Representation{
			RepresentationHTML, RepresentationSSE,
			RepresentationAtom, RepresentationJSONFeed,
		}},
	} {
		got, ok := found[tc.route]
		if !ok {
			t.Errorf("Representations has no entry for %s", tc.route)

			continue
		}

		for _, want := range tc.want {
			if !slices.Contains(got, want) {
				t.Errorf("%s: missing %s (got %v)", tc.route, want, got)
			}
		}

		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want exactly %v", tc.route, got, tc.want)
		}
	}
}

// TestRepresentationsAreRegisteredRoutes pins this inventory to the route one,
// so a pattern only one of the two parsers can see is an error rather than a
// silent gap.
func TestRepresentationsAreRegisteredRoutes(t *testing.T) {
	found, err := Representations()
	if err != nil {
		t.Fatalf("Representations: %v", err)
	}

	routes, err := Routes()
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}

	registered := make(map[string]bool, len(routes))
	for _, route := range routes {
		registered[route.String()] = true
	}

	for route, declared := range found {
		if !registered[route] {
			t.Errorf("Representations reports %s, which is not a registered route", route)
		}

		if len(declared) == 0 {
			t.Errorf("%s is in the inventory with no representations at all", route)
		}
	}
}

// TestRepresentationsRejectsAnUnreadableFeedArgument is the guard that makes
// the inventory trustworthy: a feedHandlers argument in a shape the parser
// cannot read must be an error, never a route quietly reported as serving
// fewer formats than it does.
func TestRepresentationsRejectsAnUnreadableFeedArgument(t *testing.T) {
	_, err := parseRepresentationsFromSource("unreadable.go", `
package api

func register(mux *http.ServeMux, h *Handlers) {
	mux.HandleFunc("GET /things", contentNegotiated(h.HandleThings, buildFeeds(), spa))
}
`)
	if err == nil {
		t.Fatal("parseRepresentationsFromSource accepted an unreadable feedHandlers argument")
	}
}

// TestRepresentationsReadsAFeedHandlersLiteral covers the third shape, which
// the two builders do not: a composite literal naming one field only.
func TestRepresentationsReadsAFeedHandlersLiteral(t *testing.T) {
	found, err := parseRepresentationsFromSource("literal.go", `
package api

func register(mux *http.ServeMux, h *Handlers) {
	mux.HandleFunc("GET /things", contentNegotiated(h.HandleThings, feedHandlers{
		atom: h.feedThings(renderAtom),
	}, spa))
}
`)
	if err != nil {
		t.Fatalf("parseRepresentationsFromSource: %v", err)
	}

	got := found["GET /things"]

	if !slices.Contains(got, RepresentationAtom) {
		t.Errorf("an atom-only feedHandlers literal did not yield Atom: %v", got)
	}

	if slices.Contains(got, RepresentationJSONFeed) {
		t.Errorf("an atom-only feedHandlers literal yielded JSONFeed: %v", got)
	}
}
