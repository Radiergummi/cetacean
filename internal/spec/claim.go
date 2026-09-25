package spec

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ImportPath is the only path a claim may come from. The gate reads claims
// without type information, so it may not be aliased and its ids must be literals.
const ImportPath = "github.com/radiergummi/cetacean/internal/spec"

// ClaimIDs returns the arguments of a call to fn that name requirements:
// Satisfies takes every one after t, Observed only the first. Anything else
// names none.
func ClaimIDs(fn string, args []ast.Expr) []ast.Expr {
	if len(args) < 2 {
		return nil
	}

	switch fn {
	case "Satisfies":
		return args[1:]
	case "Observed":
		return args[1:2]
	default:
		return nil
	}
}

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

	recordOnPass(t, "", ids...)
}

// Observed records what the test saw the server do, on the t that claimed the
// requirement or one of its subtests. Call it after the assertion, with the
// value that was asserted; an observation with no matching claim is dropped.
func Observed(t testing.TB, id, format string, args ...any) {
	t.Helper()

	recordOnPass(t, "\t"+sanitise(fmt.Sprintf(format, args...)), id)
}

// recordOnPass validates the ids and writes one "id TAB name" line per id, plus
// suffix, when the test passes. Withholding the record from a test that failed
// or skipped is the invariant the claims format rests on.
func recordOnPass(t testing.TB, suffix string, ids ...string) {
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

	records := make([]string, 0, len(ids))
	for _, id := range ids {
		records = append(records, id+"\t"+name+suffix)
	}

	t.Cleanup(func() {
		if t.Failed() || t.Skipped() {
			return
		}

		if err := appendRecord(dir, records...); err != nil {
			t.Errorf("spec: recording claims: %v", err)
		}
	})
}

var oneField = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")

// sanitise keeps a record on one line and in its own field: the claims format
// is id TAB name TAB observation LF with no escaping, so either character
// would split it.
func sanitise(s string) string {
	return oneField.Replace(s)
}

// appendRecord writes one file per process. Separate processes need no
// coordination — an O_APPEND write is atomic — but the tests of one package
// run concurrently, and claimMu is what keeps their records whole.
func appendRecord(dir string, records ...string) error {
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
	for _, record := range records {
		b.WriteString(record)
		b.WriteByte('\n')
	}

	_, err = f.WriteString(b.String())

	return err
}

// Evidence is one test's record against a requirement: that it passed, and
// what it saw while doing so. Observations is empty for a test that claimed
// without observing, which is every test that predates Observed.
type Evidence struct {
	Test         string
	Observations []string
}

// ReadClaims collects every record written into dir, mapping a requirement to
// the tests that recorded it. An observation is joined to the claim from the
// same test; one arriving without a claim is dropped.
func ReadClaims(dir string) (map[string][]Evidence, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.claims"))
	if err != nil {
		return nil, err
	}

	var (
		claimed  = map[string][]string{}
		observed = map[string][]record{}
	)

	for _, f := range files {
		body, err := os.ReadFile(f) // #nosec G304 -- the claims directory again
		if err != nil {
			return nil, err
		}

		for line := range strings.Lines(string(body)) {
			id, rest, ok := strings.Cut(strings.TrimRight(line, "\n"), "\t")
			if !ok || id == "" {
				continue
			}

			name, observation, isObservation := strings.Cut(rest, "\t")

			switch {
			case !isObservation:
				claimed[id] = append(claimed[id], name)
			case observation != "":
				observed[id] = append(observed[id], record{name, observation})
			}
		}
	}

	out := make(map[string][]Evidence, len(claimed))

	for id, names := range claimed {
		for _, name := range names {
			out[id] = append(out[id], Evidence{
				Test:         name,
				Observations: under(observed[id], name),
			})
		}
	}

	return out, nil
}

type record struct{ name, observation string }

// under returns the observations a claiming test is responsible for: its own,
// and those of its subtests. A claim rides on the t at the top of the test and
// an assertion usually rides on a subtest's, so the two names agree on a
// prefix rather than exactly.
func under(records []record, claim string) []string {
	var out []string

	for _, r := range records {
		if r.name == claim || strings.HasPrefix(r.name, claim+"/") {
			out = append(out, r.observation)
		}
	}

	return out
}
