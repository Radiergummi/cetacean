// Command spec-gate checks the requirement registry against the test suite.
// Output follows scripts/dump-errors: plain stderr, not slog.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: spec-gate <static|sweep|report|mutants> [flags]")
		os.Exit(2)
	}

	var err error

	switch os.Args[1] {
	case "static":
		err = runStatic(".")
	case "sweep":
		err = runSweep(".")
	case "report":
		err = report_(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "spec-gate:", err)
		os.Exit(1)
	}
}

// report_ parses the report command's own flags. The claims directory is where
// a run wrote its records; the suites are which lanes that run included, which
// is what tells a requirement nothing exercised from one nothing ran.
func report_(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	claims := fs.String("claims", ".spec-claims", "directory holding the run's .claims files")
	suites := fs.String("suites", "unit", "comma-separated lanes this run included")

	if err := fs.Parse(args); err != nil {
		return err
	}

	included := map[string]bool{}
	for name := range strings.SplitSeq(*suites, ",") {
		if name = strings.TrimSpace(name); name != "" {
			included[name] = true
		}
	}

	return runReport(".", *claims, included)
}
