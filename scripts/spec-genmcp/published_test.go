package main

import (
	"slices"
	"testing"
)

func TestNormalizeLeavesCodeAlone(t *testing.T) {
	for in, want := range map[string]string{
		"the server **MUST** include `_meta`":         "the server MUST include _meta",
		"a _notification_ before `snake_case_name`":   "a notification before snake_case_name",
		"see [Server Validation](#server-validation)": "see Server Validation",
		"see [the registry][deprecated]":              "see the registry",
		"<b>Cache Scope Field</b>,`cacheScope`,":      "Cache Scope Field,cacheScope,",
		"Client\\<\\>Server":                          "Client<>Server",
	} {
		if got := normalize(in, false); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInsertionsAreDroppedOutsideCode(t *testing.T) {
	got := normalize("MUST be 400 [for X], keyed by `data[\"k\"]`", true)
	if want := `MUST be 400, keyed by data["k"]`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSentencesKeepAbbreviationsWhole(t *testing.T) {
	got := sentences("Use e.g. Foo here. Then Bar. done")
	if want := []string{"Use e.g. Foo here.", "Then Bar. done"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHeadingsGetGitHubAnchors(t *testing.T) {
	seen := map[string]int{}

	for i, want := range []struct{ in, want string }{
		{"Security & Endpoint", "security--endpoint"},
		{"_meta", "_meta"},
		{"Response", "response"},
		{"Response", "response-1"},
	} {
		if got := uniqueSlug(seen, want.in); got != want.want {
			t.Errorf("%d: slug(%q) = %q, want %q", i, want.in, got, want.want)
		}
	}
}

func TestAQuoteHoldsOnlyInOrder(t *testing.T) {
	p := parsePage(pageRef{key: "p"}, "# T\n\nAlpha one. Beta two.\n\n- Gamma\n\n```\nDelta\n```\n")

	s, ok := p.section("t")
	if !ok {
		t.Fatalf("no section t in %+v", p.sections)
	}

	for quote, want := range map[string]bool{
		"Alpha one. [...] Gamma": true,
		"Gamma [...] Alpha one":  false,
		"Delta":                  false,
	} {
		if got := s.holds(quote); got != want {
			t.Errorf("holds(%q) = %v, want %v", quote, got, want)
		}
	}
}
