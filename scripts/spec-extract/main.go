// Command spec-extract reads the specifications the registry transcribes and
// reports the normative statements no entry accounts for. inventory.count is
// otherwise a number chosen by whoever chose the requirements it counts, so
// requirements + dismissed = count holds by construction and a clause nobody
// read is indistinguishable from one that does not apply.
//
// It needs the network and is not part of `make check`: the sentence splitter
// is approximate, so the output is a list to reconcile rather than a verdict.
// What is committed is scripts/spec-extract/baseline.yaml — the statements
// themselves, not a count of them, so that registering one while another
// drifts out of range is a diff rather than a number that did not move.
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/radiergummi/cetacean/internal/spec"
)

const baselinePath = "scripts/spec-extract/baseline.yaml"

const baselineHeader = `# The normative statements each document carries that no requirement quotes.
# Most are held by a dismissal, which is one line of prose with nothing to
# match against; the rest are the finding. Regenerate with spec-extract --write,
# and shrink this file only by registering or dismissing what it lists.
`

func main() {
	check := flag.Bool("check", false, "fail on a statement the baseline does not have")
	write := flag.Bool("write", false, "rewrite the baseline from this run")
	verbose := flag.Bool("v", false, "print every unheld statement, not just the count")
	flag.Parse()

	if err := run(*check, *write, *verbose, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "spec-extract:", err)
		os.Exit(1)
	}
}

// baseline holds the statements each document carries that no requirement
// quotes. Committed as the statements rather than as a count of them: a count
// says nothing about which ones, so registering one while a second drifts out
// of quoting range leaves it unchanged and the drift invisible.
func baseline() (map[string][]string, error) {
	body, err := os.ReadFile(baselinePath)
	if err != nil {
		return nil, err
	}

	out := map[string][]string{}

	return out, yaml.Unmarshal(body, &out)
}

// drifted returns the statements this run has that the baseline does not.
// The other direction is not a failure — it is a statement somebody
// registered — but it is reported, because the file wants rewriting.
func drifted(got, want []string) (added, gone []string) {
	have := make(map[string]bool, len(want))
	for _, s := range want {
		have[s] = true
	}

	seen := make(map[string]bool, len(got))

	for _, s := range got {
		seen[s] = true

		if !have[s] {
			added = append(added, s)
		}
	}

	for _, s := range want {
		if !seen[s] {
			gone = append(gone, s)
		}
	}

	return added, gone
}

func run(check, write, verbose bool, only []string) error {
	reg, err := spec.Load()
	if err != nil {
		return err
	}

	base, err := baseline()
	if err != nil {
		return err
	}

	var (
		failures []string
		reported int
		found    = map[string][]string{}
	)

	for _, doc := range reg.Documents {
		key := doc.Key()

		if len(only) > 0 && !slices.Contains(only, key) {
			continue
		}

		if doc.Inventory == nil || len(doc.Inventory.Sections) == 0 {
			continue
		}

		unheld, err := report(doc, verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", key, err)

			continue
		}

		reported++
		found[key] = unheld

		added, gone := drifted(unheld, base[key])
		for _, s := range gone {
			fmt.Fprintf(os.Stderr, "    now quoted, drop from the baseline: %s\n", truncate(s))
		}

		if check {
			for _, s := range added {
				failures = append(failures, fmt.Sprintf("%s: %s", key, truncate(s)))
			}
		}
	}

	if reported == 0 {
		return fmt.Errorf("no document declares inventory.sections")
	}

	if write {
		return rewrite(base, found, only)
	}

	for _, f := range failures {
		fmt.Fprintln(os.Stderr, "  no requirement quotes this and the baseline does not have it:")
		fmt.Fprintln(os.Stderr, "    "+f)
	}

	if len(failures) > 0 {
		return fmt.Errorf("%d statement(s) nothing accounts for", len(failures))
	}

	return nil
}

// rewrite replaces the baseline with what this run read, keeping the documents
// it did not look at.
func rewrite(base, found map[string][]string, only []string) error {
	if len(only) == 0 {
		base = map[string][]string{}
	}

	// One declaration restated in two sections is two statements and one line
	// here: the diff is by set, so a second copy adds no signal.
	for key, unheld := range found {
		base[key] = slices.Compact(unheld)
	}

	body, err := yaml.Marshal(base)
	if err != nil {
		return err
	}

	return os.WriteFile(baselinePath, append([]byte(baselineHeader), body...), 0o600)
}

// report prints one document's reconciliation and returns how many of its
// statements no requirement quotes.
func report(doc *spec.Document, verbose bool) ([]string, error) {
	url, err := sourceURL(doc)
	if err != nil {
		return nil, err
	}

	body, err := fetch(url)
	if err != nil {
		return nil, err
	}

	statements := Statements(body, doc.Inventory.Sections)

	unheld, unquoted := Reconcile(statements, doc)
	slices.Sort(unheld)

	accounted := len(doc.Requirements) + len(doc.Dismissed)

	fmt.Fprintf(os.Stderr,
		"%s §%s: %d normative statement(s); %d quoted by a requirement, %d not. "+
			"Registry accounts for %d (%d requirements + %d dismissed), inventory.count says %d\n",
		doc.Key(), strings.Join(doc.Inventory.Sections, ", §"),
		len(statements), len(statements)-len(unheld), len(unheld),
		accounted, len(doc.Requirements), len(doc.Dismissed), doc.Inventory.Count,
	)

	// An entry matching nothing is quoting prose the document does not state
	// normatively, or prose it no longer states at all. Always printed: this
	// is transcription drift, which nothing else in the registry can see.
	for _, id := range unquoted {
		fmt.Fprintf(os.Stderr, "    quotes nothing in the declared sections: %s\n", id)
	}

	if !verbose {
		return unheld, nil
	}

	for _, s := range unheld {
		fmt.Fprintf(os.Stderr, "    unheld: %s\n", truncate(s))
	}

	return unheld, nil
}

func truncate(s string) string {
	const width = 140

	if len(s) <= width {
		return s
	}

	return s[:width] + "…"
}
