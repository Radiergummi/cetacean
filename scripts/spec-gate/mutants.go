package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/radiergummi/cetacean/internal/spec"
)

// applyMutant edits the one occurrence of the pattern. Anything else — none,
// or several — means the catalog no longer describes the code it claims to.
func applyMutant(src string, m spec.Mutant) (string, error) {
	switch n := strings.Count(src, m.Replace); n {
	case 1:
		return strings.Replace(src, m.Replace, m.With, 1), nil
	case 0:
		return "", fmt.Errorf("pattern not found: %q", m.Replace)
	default:
		return "", fmt.Errorf("pattern occurs %d times, want exactly one: %q", n, m.Replace)
	}
}

// overlayFor writes the mutated source and the -overlay file naming it, so the
// tree on disk is never touched.
func overlayFor(dir, file, mutated string) (string, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}

	scratch := filepath.Join(dir, filepath.Base(file))

	// #nosec G703 -- dir is this process's own scratch directory and the name
	// is one path element taken from the embedded registry, never a request.
	if err := os.WriteFile(scratch, []byte(mutated), 0o600); err != nil {
		return "", err
	}

	body, err := json.Marshal(struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: map[string]string{abs: scratch}})
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, "overlay.json")

	return path, os.WriteFile(path, body, 0o600)
}

// claimants returns the tests that claim this requirement from outside a build
// tag, with the packages they live in. An e2e-tagged claimant needs the whole
// Docker environment, so a mutant with only those cannot be verified here and
// must not be reported as killed.
func claimants(claims []Claim, id string) (names, pkgs []string) {
	for _, c := range claims {
		if c.ID != id || c.Tagged {
			continue
		}

		names = append(names, c.Func)
		pkgs = append(pkgs, "./"+filepath.Dir(c.File)+"/")
	}

	return slices.Compact(slices.Sorted(slices.Values(names))),
		slices.Compact(slices.Sorted(slices.Values(pkgs)))
}

func runMutants(root string) error {
	reg, err := spec.Load()
	if err != nil {
		return err
	}

	claims, errs := Scan(root)
	if len(errs) > 0 {
		report(errs)

		return fmt.Errorf("%d problem(s) scanning for claims", len(errs))
	}

	var ran int

	for _, q := range reg.All() {
		if len(q.Mutants) == 0 {
			continue
		}

		id := q.FullID()

		names, pkgs := claimants(claims, id)
		if len(names) == 0 {
			errs = append(errs, fmt.Errorf(
				"%s: has mutants but no claimant outside a build tag; nothing here can kill them",
				id))

			continue
		}

		for _, m := range q.Mutants {
			ran++

			if err := kill(root, id, m, names, pkgs); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		report(errs)

		return fmt.Errorf("%d problem(s)", len(errs))
	}

	fmt.Fprintf(os.Stderr, "spec: %d mutant(s) killed by their requirements' tests\n", ran)

	return nil
}

// kill runs the requirement's claimants against the mutated tree. The test
// command exiting 0 means the mutant survived: the tests pass whether or not
// the requirement holds, so they are not what is enforcing it.
func kill(root, id string, m spec.Mutant, names, pkgs []string) error {
	path := filepath.Join(root, m.File)

	src, err := os.ReadFile(path) // #nosec G304 -- a path from the embedded registry
	if err != nil {
		return fmt.Errorf("%s: %w", id, err)
	}

	mutated, err := applyMutant(string(src), m)
	if err != nil {
		return fmt.Errorf("%s: %s: %w", id, m.File, err)
	}

	dir, err := os.MkdirTemp("", "spec-mutant-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir) //nolint:errcheck // scratch directory

	overlay, err := overlayFor(dir, path, mutated)
	if err != nil {
		return fmt.Errorf("%s: %w", id, err)
	}

	// #nosec G204 -- every argument comes from the embedded registry or the
	// claim scan of this repository's own test files.
	argv := append([]string{
		"test",
		"-overlay=" + overlay,
		"-count=1",
		"-run=^(" + strings.Join(names, "|") + ")$",
	}, pkgs...)

	cmd := exec.CommandContext(context.Background(), "go", argv...)
	cmd.Dir = root

	out, err := cmd.CombinedOutput()

	// A mutant that does not compile proves nothing about the tests: go test
	// exits non-zero either way, so the two have to be told apart here.
	if strings.Contains(string(out), "[build failed]") {
		return fmt.Errorf("%s: mutant does not compile (%s)\n%s", id, m.File, out)
	}

	// A claimant that is not a top-level Test matches no -run filter, and a
	// run of nothing exits 0 exactly as a surviving mutant does.
	if strings.Contains(string(out), "no tests to run") {
		return fmt.Errorf(
			"%s: no claimant matched -run; %s must name top-level tests",
			id, strings.Join(names, ", "),
		)
	}

	if err != nil {
		return nil
	}

	return fmt.Errorf("%s: mutant survived (%s: %s)\n%s", id, m.File, m.Replace, out)
}
