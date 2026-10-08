package main

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/radiergummi/cetacean/internal/spec"
)

// citationRE matches how specifications are named in comments, prose and links.
// Case-insensitive because an rfc-editor.org URL spells the number in lower
// case. OAuth 2.1 has no number yet: it is an Internet-Draft, named by title.
var citationRE = regexp.MustCompile(
	`(?i)\b(RFC[ -]?[0-9]{3,4}|SEP[ -]?[0-9]{3,4}|OAuth[ -]?2\.1)\b`,
)

// mechanismPrefixes are this mechanism's own files: the registry states each
// token to register or dismiss it, the gate's tests use them as fixtures, and
// spec-extract quotes the documents. Counting them would let a token cite
// itself and hide a stale dismissal.
var mechanismPrefixes = []string{
	"internal/spec/registry/",
	"scripts/spec-gate/",
	"scripts/spec-vet/",
	"scripts/spec-extract/",
}

// Citations maps each specification the tree names to the files naming it.
// Every tracked file counts: a citation in the root binary or the changelog is
// as much an unaccounted specification as one in internal/.
func Citations(root string) (map[string][]string, error) {
	tracked, err := gitLsFiles(root)
	if err != nil {
		return nil, err
	}

	cited := map[string][]string{}

	for _, rel := range tracked {
		if slices.ContainsFunc(mechanismPrefixes, func(p string) bool {
			return strings.HasPrefix(rel, p)
		}) {
			continue
		}

		body, err := os.ReadFile(root + "/" + rel)
		if err != nil {
			continue
		}

		seen := map[string]bool{}

		for _, m := range citationRE.FindAllString(string(body), -1) {
			token := spec.Token(m)
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

	raw, err := spec.Unregistered()
	if err != nil {
		return []error{err}
	}

	// Both sides canonicalise, or a dismissal spelled "RFC 1918" never meets
	// the citation it answers.
	dismissed := make(map[string]string, len(raw))
	for name, reason := range raw {
		dismissed[spec.Token(name)] = reason
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

	for token, reason := range dismissed {
		if registered[token] {
			errs = append(errs, fmt.Errorf(
				"%s is registered, so its registry/unregistered.yaml dismissal (%q) is dead",
				token, reason,
			))

			continue
		}

		if len(cited[token]) == 0 {
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
