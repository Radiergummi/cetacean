package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ClaimsEnv names the directory claims are written to. Unset, Satisfies
// validates and writes nothing, which is every ordinary `go test` run.
const ClaimsEnv = "CETACEAN_SPEC_CLAIMS"

var claimMu sync.Mutex

// Satisfies records that the calling test exercises these requirements.
// Call it BEFORE any helper that registers a cleanup: cleanups run
// last-registered-first, so an assertion deferred by an earlier one would
// run after this claim and could not withhold it.
func Satisfies(t testing.TB, ids ...string) {
	t.Helper()

	reg, err := Load()
	if err != nil {
		t.Fatalf("spec: %v", err)
	}

	for _, id := range ids {
		if _, ok := reg.Lookup(id); !ok {
			t.Fatalf("spec: no requirement %q in the registry", id)
		}
	}

	dir := os.Getenv(ClaimsEnv)
	if dir == "" {
		return
	}

	name := sanitise(t.Name())

	t.Cleanup(func() {
		if t.Failed() || t.Skipped() {
			return
		}

		if err := appendClaims(dir, name, ids); err != nil {
			t.Errorf("spec: recording claims: %v", err)
		}
	})
}

// sanitise keeps a name on one line: the claims format is id TAB name LF with
// no escaping, so either character would split the record.
func sanitise(name string) string {
	return strings.NewReplacer("\t", "_", "\n", "_", "\r", "_").Replace(name)
}

// appendClaims writes one file per process. Separate processes need no
// coordination — an O_APPEND write is atomic — but the tests of one package
// run concurrently, and claimMu is what keeps their records whole.
func appendClaims(dir, name string, ids []string) error {
	// #nosec G703 -- the claims directory is a test-harness path taken from
	// the environment by the developer running the suite, never a request.
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	path := filepath.Join(dir, fmt.Sprintf("%d.claims", os.Getpid()))

	claimMu.Lock()
	defer claimMu.Unlock()

	// #nosec G703 -- same claims directory, opened for this process's file.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // append-only claims file

	var b strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&b, "%s\t%s\n", id, name)
	}

	_, err = f.WriteString(b.String())

	return err
}

// ReadClaims collects every claim written into dir, mapping a requirement to
// the tests that recorded it. It is the reader for appendClaims' format, and
// lives beside it so the two cannot drift.
func ReadClaims(dir string) (map[string][]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.claims"))
	if err != nil {
		return nil, err
	}

	out := map[string][]string{}

	for _, f := range files {
		body, err := os.ReadFile(f) // #nosec G304 -- the claims directory again
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
