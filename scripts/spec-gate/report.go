package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/radiergummi/cetacean/internal/spec"
)

// Summary is what one invocation of the suite exercised. Uncovered is the only
// failing category: a gap and a deferral are decisions already recorded, and
// "not run" is a lane this invocation did not include.
type Summary struct {
	Total     int
	Exercised int
	NotRun    int
	Gaps      int
	Deferred  int
	Uncovered []string
}

// summarise classifies every requirement against what this invocation ran.
func summarise(reg *spec.Registry, static []Claim, ran map[string][]string, e2e bool) Summary {
	tagged := map[string]bool{}
	untagged := map[string]bool{}

	for _, c := range static {
		if c.Tagged {
			tagged[c.ID] = true
		} else {
			untagged[c.ID] = true
		}
	}

	out := Summary{}

	for _, q := range reg.All() {
		id := q.FullID()
		out.Total++

		switch {
		case q.Deferred != "":
			out.Deferred++
		case q.Gap != "":
			out.Gaps++
		case len(ran[id]) > 0:
			out.Exercised++
		case tagged[id] && !untagged[id] && !e2e:
			out.NotRun++
		default:
			out.Uncovered = append(out.Uncovered, id)
		}
	}

	slices.Sort(out.Uncovered)

	return out
}

func runReport(root, claims, suites string) error {
	reg, err := spec.Load()
	if err != nil {
		return err
	}

	static, errs := Scan(root)
	if len(errs) > 0 {
		report(errs)

		return fmt.Errorf("%d problem(s) scanning for claims", len(errs))
	}

	ran, err := spec.ReadClaims(claims)
	if err != nil {
		return err
	}

	cited, err := Citations(root)
	if err != nil {
		return err
	}

	summary := summarise(reg, static, ran, strings.Contains(suites, "e2e"))

	// Never a bare ratio: a number without its denominator reads as a
	// compliance claim no suite here can support.
	fmt.Fprintf(os.Stderr,
		"spec: %d requirements across %d documents (%d of %d cited specifications)\n",
		summary.Total, len(reg.Documents), len(reg.Documents), len(cited))
	fmt.Fprintf(os.Stderr,
		"  %d exercised by %s, %d not run, %d gaps, %d deferred, %d uncovered\n",
		summary.Exercised, suites,
		summary.NotRun, summary.Gaps, summary.Deferred, len(summary.Uncovered))

	for _, id := range summary.Uncovered {
		fmt.Fprintln(os.Stderr, "  uncovered:", id)
	}

	if len(summary.Uncovered) > 0 {
		return fmt.Errorf(
			"%d requirement(s) claimed by a test that did not run",
			len(summary.Uncovered),
		)
	}

	return nil
}
