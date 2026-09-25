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
	Exercised int
	NotRun    int
	Gaps      []string
	Deferred  int
	Uncovered []string

	// Observed counts the exercised requirements a test recorded a value
	// against. Reported separately from Exercised because the two answer
	// different questions: that a test passed, and what the server did.
	Observed int
}

// summarise classifies every requirement against what this invocation ran. The
// lane comes from the registry rather than from where the claim was written:
// the static gate already holds the two to each other, and reading the
// declaration keeps the count answerable without parsing a file.
func summarise(reg *spec.Registry, ran map[string][]spec.Evidence, e2e bool) Summary {
	out := Summary{}

	for _, q := range reg.All() {
		id := q.FullID()

		switch {
		case q.Deferred != "":
			out.Deferred++
		case q.Gap != "":
			out.Gaps = append(out.Gaps, id)
		case len(ran[id]) > 0:
			out.Exercised++

			if len(observations(ran[id])) > 0 {
				out.Observed++
			}
		case q.Lane == spec.LaneE2E && !e2e:
			out.NotRun++
		default:
			out.Uncovered = append(out.Uncovered, id)
		}
	}

	slices.Sort(out.Gaps)
	slices.Sort(out.Uncovered)

	return out
}

func runReport(root, claims, suites string, transcript bool) error {
	reg, err := spec.Load()
	if err != nil {
		return err
	}

	if _, _, errs := Scan(root); len(errs) > 0 {
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

	summary := summarise(reg, ran, strings.Contains(suites, "e2e"))

	// Only a document the tree actually cites belongs in the numerator: the
	// denominator counts citations, and a family named nowhere is in neither.
	covered := 0

	for _, d := range reg.Documents {
		if len(cited[d.Token()]) > 0 {
			covered++
		}
	}

	// Never a bare ratio: a number without its denominator reads as a
	// compliance claim no suite here can support.
	fmt.Fprintf(os.Stderr,
		"spec: %d requirements across %d documents (%d of %d cited specifications)\n",
		len(reg.All()), len(reg.Documents), covered, len(cited))
	fmt.Fprintf(os.Stderr,
		"  %d exercised by %s, %d not run, %d gaps, %d deferred, %d uncovered\n",
		summary.Exercised, suites,
		summary.NotRun, len(summary.Gaps), summary.Deferred, len(summary.Uncovered))
	fmt.Fprintf(os.Stderr,
		"  %d of the %d exercised recorded what the server answered\n",
		summary.Observed, summary.Exercised)

	// Named, not counted. A gap is the one state nothing pins, so the only
	// thing keeping it from accumulating unread is that every run says which
	// requirements are in it.
	for _, id := range summary.Gaps {
		fmt.Fprintln(os.Stderr, "  gap:", id)
	}

	for _, id := range summary.Uncovered {
		fmt.Fprintln(os.Stderr, "  uncovered:", id)
	}

	if transcript {
		writeTranscript(reg, ran)
	}

	if len(summary.Uncovered) > 0 {
		return fmt.Errorf(
			"%d requirement(s) nothing in this run exercised",
			len(summary.Uncovered),
		)
	}

	return nil
}

// observations flattens the values every claiming test recorded.
func observations(evidence []spec.Evidence) []string {
	var out []string
	for _, e := range evidence {
		out = append(out, e.Observations...)
	}

	return out
}

// writeTranscript prints each requirement beside what a test watched the
// server do about it. This is the only output here that answers the question
// a reader outside the repository actually has, so it quotes the requirement
// rather than naming it.
func writeTranscript(reg *spec.Registry, ran map[string][]spec.Evidence) {
	for _, q := range reg.All() {
		id := q.FullID()

		values := observations(ran[id])
		if len(values) == 0 {
			continue
		}

		fmt.Fprintf(os.Stdout, "\n%s [%s]\n  %s\n", id, q.Level, strings.Join(
			strings.Fields(q.Text), " "))

		for _, v := range slices.Compact(slices.Sorted(slices.Values(values))) {
			fmt.Fprintf(os.Stdout, "  observed: %s\n", v)
		}
	}
}
