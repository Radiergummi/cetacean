package main

import (
	"strings"
	"testing"
)

func TestNormaliseCitation(t *testing.T) {
	for in, want := range map[string]string{
		"RFC 7636": "RFC7636",
		"RFC7636":  "RFC7636",
		"SEP-2575": "SEP-2575",
		"SEP 2575": "SEP-2575",
		"SEP2575":  "SEP-2575",
	} {
		if got := normaliseCitation(in); got != want {
			t.Errorf("normaliseCitation(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestACitedButUnregisteredSpecificationFailsTheSweep(t *testing.T) {
	cited := map[string][]string{
		"RFC7636": {"internal/oauth/server.go"},
		"RFC7009": {"internal/oauth/server.go"},
	}

	registered := map[string]bool{"RFC7636": true}
	dismissed := map[string]string{}

	errs := sweepErrors(cited, registered, dismissed)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "RFC7009") {
		t.Fatalf("errors = %v, want one naming RFC7009", errs)
	}
}

func TestADismissalNeedsAReason(t *testing.T) {
	cited := map[string][]string{"RFC3339": {"internal/api/x.go"}}

	errs := sweepErrors(cited, map[string]bool{}, map[string]string{"RFC3339": "  "})
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one about the empty reason", errs)
	}
}

func TestAStaleDismissalFailsTheSweep(t *testing.T) {
	errs := sweepErrors(
		map[string][]string{},
		map[string]bool{},
		map[string]string{"RFC9999": "nothing cites this any more"},
	)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "RFC9999") {
		t.Fatalf("errors = %v, want one about the stale dismissal", errs)
	}
}
