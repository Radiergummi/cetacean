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

	recordOnPass(t, func(name string) []string {
		records := make([]string, 0, len(ids))
		for _, id := range ids {
			records = append(records, id+"\t"+name)
		}

		return records
	}, ids...)
}

// Observed records what the test saw the server do — the status code, the
// error code, the header the requirement is about. Call it after the
// assertion, with the value that was asserted, on the t that claimed the
// requirement or on one of its subtests. An observation the report cannot join
// to a claim is dropped: only Satisfies establishes that the test exercising
// the requirement passed, and only it is a form the static gate can read.
//
// This is the only evidence in the registry that names a behaviour rather than
// a test. A claim says a test ran and passed; a mutant says the test refuses an
// edit; neither says what the server answered.
func Observed(t testing.TB, id, format string, args ...any) {
	t.Helper()

	observation := sanitise(fmt.Sprintf(format, args...))

	recordOnPass(t, func(name string) []string {
		return []string{id + "\t" + name + "\t" + observation}
	}, id)
}

// recordOnPass validates the ids and arranges for lines to be written when the
// test passes. Both entry points share it rather than each holding a copy:
// withholding the record from a test that failed or skipped is the one
// invariant the whole claims format rests on, and it may not drift between
// them.
func recordOnPass(t testing.TB, lines func(name string) []string, ids ...string) {
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

	records := lines(sanitise(t.Name()))

	t.Cleanup(func() {
		if t.Failed() || t.Skipped() {
			return
		}

		if err := appendRecord(dir, records...); err != nil {
			t.Errorf("spec: recording claims: %v", err)
		}
	})
}

// sanitise keeps a record on one line and in its own field: the claims format
// is id TAB name TAB observation LF with no escaping, so either character
// would split it.
func sanitise(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
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
// the tests that recorded it. It is the reader for appendRecord's format, and
// lives beside it so the two cannot drift.
//
// An observation is joined to the claim from the same test. One arriving
// without a claim is dropped: only Satisfies establishes that the test
// exercising the requirement passed, and only it is a form the static gate
// can see.
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
