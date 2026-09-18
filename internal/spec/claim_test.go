package spec

import (
	"slices"
	"strings"
	"testing"
)

const knownID = "oauth/rfc7636/verifier-must-match-challenge"

// readClaims returns every "id<TAB>name" record written into dir.
func readClaims(t *testing.T, dir string) []string {
	t.Helper()

	claims, err := ReadClaims(dir)
	if err != nil {
		t.Fatal(err)
	}

	var lines []string

	for id, names := range claims {
		for _, name := range names {
			lines = append(lines, id+"\t"+name)
		}
	}

	slices.Sort(lines)

	return lines
}

func TestSatisfiesRecordsAClaimForAPassingTest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ClaimsEnv, dir)

	t.Run("passing", func(t *testing.T) {
		Satisfies(t, knownID)
	})

	got := readClaims(t, dir)
	if len(got) != 1 || !strings.HasPrefix(got[0], knownID+"\t") {
		t.Fatalf("claims = %q, want one line for %s", got, knownID)
	}
}

// A requirement whose only evidence skipped on this machine has not been
// checked on this machine.
func TestSatisfiesWithholdsAClaimFromASkippedTest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ClaimsEnv, dir)

	t.Run("skipping", func(t *testing.T) {
		Satisfies(t, knownID)
		t.Skip("environmental")
	})

	if got := readClaims(t, dir); len(got) != 0 {
		t.Fatalf("claims = %q, want none", got)
	}
}

func TestSatisfiesWithholdsAClaimFromAFailingTest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ClaimsEnv, dir)

	fake := &recordingTB{TB: t}
	Satisfies(fake, knownID)
	fake.failed = true
	fake.runCleanups()

	if got := readClaims(t, dir); len(got) != 0 {
		t.Fatalf("claims = %q, want none", got)
	}
}

func TestSatisfiesWritesNothingWithoutTheEnvironmentVariable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ClaimsEnv, "")

	t.Run("passing", func(t *testing.T) {
		Satisfies(t, knownID)
	})

	if got := readClaims(t, dir); len(got) != 0 {
		t.Fatalf("claims = %q, want none", got)
	}
}

// recordingTB drives the cleanup path with an outcome a real subtest cannot be
// made to have without failing the run.
type recordingTB struct {
	testing.TB

	failed   bool
	cleanups []func()
}

func (r *recordingTB) Failed() bool      { return r.failed }
func (r *recordingTB) Skipped() bool     { return false }
func (r *recordingTB) Name() string      { return "TestRecording" }
func (r *recordingTB) Cleanup(fn func()) { r.cleanups = append(r.cleanups, fn) }
func (r *recordingTB) Helper()           {}
func (r *recordingTB) runCleanups() {
	for i := range slices.Backward(r.cleanups) {
		r.cleanups[i]()
	}
}

func TestReadClaimsGroupsEveryRecordByRequirement(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ClaimsEnv, dir)

	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			Satisfies(t, knownID)
		})
	}

	got, err := ReadClaims(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(got[knownID]) != 2 {
		t.Fatalf("claims = %v, want two records for %s", got, knownID)
	}
}
