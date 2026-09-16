package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// ReadClaims collects every claim written into dir, mapping a requirement to
// the tests that recorded it.
func ReadClaims(dir string) (map[string][]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.claims"))
	if err != nil {
		return nil, err
	}

	out := map[string][]string{}

	for _, f := range files {
		body, err := os.ReadFile(f) // #nosec G304 -- a claims directory named on the command line
		if err != nil {
			return nil, err
		}

		for line := range strings.Lines(string(body)) {
			id, name, ok := strings.Cut(strings.TrimRight(line, "\n"), "\t")
			if !ok || id == "" {
				continue
			}

			out[id] = append(out[id], name)
		}
	}

	return out, nil
}

// summarise classifies every requirement against what this invocation ran.
func summarise(
	reg *spec.Registry,
	static []Claim,
	ran map[string][]string,
	suites map[string]bool,
) Summary {
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
		case tagged[id] && !untagged[id] && !suites["e2e"]:
			out.NotRun++
		default:
			out.Uncovered = append(out.Uncovered, id)
		}
	}

	sort.Strings(out.Uncovered)

	return out
}

func runReport(root, claims string, suites map[string]bool) error {
	reg, err := spec.Load()
	if err != nil {
		return err
	}

	static, errs := Scan(root)
	if len(errs) > 0 {
		report(errs)

		return fmt.Errorf("%d problem(s) scanning for claims", len(errs))
	}

	ran, err := ReadClaims(claims)
	if err != nil {
		return err
	}

	cited, err := Citations(root)
	if err != nil {
		return err
	}

	summary := summarise(reg, static, ran, suites)

	// Never a bare ratio: a number without its denominator reads as a
	// compliance claim no suite here can support.
	fmt.Fprintf(os.Stderr,
		"spec: %d requirements across %d documents (%d of %d cited specifications)\n",
		summary.Total, len(reg.Documents), len(reg.Documents), len(cited))
	fmt.Fprintf(os.Stderr,
		"  %d exercised by %s, %d not run, %d gaps, %d deferred, %d uncovered\n",
		summary.Exercised, strings.Join(suiteNames(suites), "+"),
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

func suiteNames(suites map[string]bool) []string {
	names := make([]string, 0, len(suites))
	for name := range suites {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}
