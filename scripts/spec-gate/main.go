// Command spec-gate checks the requirement registry against the test suite.
// Output follows scripts/dump-errors: plain stderr, not slog.
package main

import (
	"fmt"
	"os"
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
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "spec-gate:", err)
		os.Exit(1)
	}
}
