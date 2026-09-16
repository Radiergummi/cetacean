package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/radiergummi/cetacean/internal/spec"
)

// citationRE matches the way specifications are named in comments and prose.
var citationRE = regexp.MustCompile(`\b(RFC[ -]?[0-9]{3,4}|SEP[ -]?[0-9]{3,4})\b`)

// sweptDirs are where we talk about specifications. Everything else is
// vendored, generated, or somebody else's prose.
var sweptDirs = []string{"internal", "docs", "api", "test"}

func normaliseCitation(in string) string {
	upper := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(in))

	if num, ok := strings.CutPrefix(upper, "SEP"); ok {
		return "SEP-" + num
	}

	return upper
}

// Citations maps each specification the tree names to the files naming it.
func Citations(root string) (map[string][]string, error) {
	args := append([]string{"-C", root, "ls-files", "-z", "--"}, sweptDirs...)

	out, err := exec.CommandContext(context.Background(), "git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	cited := map[string][]string{}

	for p := range bytes.SplitSeq(out, []byte{0}) {
		if len(p) == 0 {
			continue
		}

		rel := string(p)

		body, err := os.ReadFile(root + "/" + rel)
		if err != nil {
			continue
		}

		seen := map[string]bool{}

		for _, m := range citationRE.FindAllString(string(body), -1) {
			token := normaliseCitation(m)
			if seen[token] {
				continue
			}

			seen[token] = true
			cited[token] = append(cited[token], rel)
		}
	}

	return cited, nil
}

func checkSweep(reg *spec.Registry, cited map[string][]string) []error {
	registered := map[string]bool{}
	for _, d := range reg.Documents {
		registered[d.Token()] = true
	}

	dismissed, err := spec.Unregistered()
	if err != nil {
		return []error{err}
	}

	return sweepErrors(cited, registered, dismissed)
}

func sweepErrors(
	cited map[string][]string,
	registered map[string]bool,
	dismissed map[string]string,
) []error {
	var errs []error

	for token, files := range cited {
		if registered[token] {
			continue
		}

		reason, ok := dismissed[token]
		if !ok {
			errs = append(errs, fmt.Errorf(
				"%s is cited (%s) but is neither registered nor listed in registry/unregistered.yaml",
				token,
				files[0],
			))

			continue
		}

		if strings.TrimSpace(reason) == "" {
			errs = append(errs, fmt.Errorf("%s: dismissal has no reason", token))
		}
	}

	for token := range dismissed {
		if len(cited[token]) == 0 && !registered[token] {
			errs = append(errs, fmt.Errorf(
				"%s is dismissed in registry/unregistered.yaml but nothing cites it any more",
				token,
			))
		}
	}

	return errs
}

func runSweep(root string) error {
	reg, err := spec.Load()
	if err != nil {
		return err
	}

	cited, err := Citations(root)
	if err != nil {
		return err
	}

	if errs := checkSweep(reg, cited); len(errs) > 0 {
		report(errs)

		return fmt.Errorf("%d unaccounted specification(s)", len(errs))
	}

	fmt.Fprintf(os.Stderr, "spec: %d specifications cited, all accounted for\n", len(cited))

	return nil
}
