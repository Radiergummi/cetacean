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

	for id, evidence := range claims {
		for _, e := range evidence {
			lines = append(lines, id+"\t"+e.Test)
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

func TestObservedJoinsTheClaimFromTheSameTest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ClaimsEnv, dir)

	t.Run("observing", func(t *testing.T) {
		Satisfies(t, knownID)
		Observed(t, knownID, "status=%d error=%s", 400, "invalid_grant")
	})

	claims, err := ReadClaims(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := claims[knownID]
	if len(got) != 1 {
		t.Fatalf("evidence = %v, want one record", got)
	}

	want := []string{"status=400 error=invalid_grant"}
	if !slices.Equal(got[0].Observations, want) {
		t.Errorf("observations = %q, want %q", got[0].Observations, want)
	}
}

// A claim rides on the t at the top of a test and an assertion on a subtest's,
// so the join is by prefix rather than by equality.
func TestAnObservationFromASubtestJoinsItsParentsClaim(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ClaimsEnv, dir)

	t.Run("parent", func(t *testing.T) {
		Satisfies(t, knownID)

		t.Run("case", func(t *testing.T) {
			Observed(t, knownID, "status=%d", 302)
		})
	})

	claims, err := ReadClaims(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := claims[knownID]
	if len(got) != 1 || !slices.Equal(got[0].Observations, []string{"status=302"}) {
		t.Fatalf("evidence = %+v, want the subtest's observation on the parent's claim", got)
	}
}

// Only Satisfies establishes that the test exercising a requirement passed, so
// an observation with no claim beside it must not make one.
func TestAnObservationWithoutAClaimIsDropped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ClaimsEnv, dir)

	t.Run("observing only", func(t *testing.T) {
		Observed(t, knownID, "status=%d", 200)
	})

	claims, err := ReadClaims(dir)
	if err != nil {
		t.Fatal(err)
	}

	if got, ok := claims[knownID]; ok {
		t.Errorf("evidence = %+v, want the requirement unclaimed", got)
	}
}

func TestAFailedTestObservesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ClaimsEnv, dir)

	failing := &recordingTB{TB: t, failed: true}
	Satisfies(failing, knownID)
	Observed(failing, knownID, "status=%d", 500)
	failing.runCleanups()

	if got := readClaims(t, dir); len(got) != 0 {
		t.Errorf("records = %q, want none", got)
	}
}

// The format has no escaping, so a value carrying a tab would open a field of
// its own and one carrying a newline would open a record.
func TestAnObservationIsKeptToItsOwnField(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ClaimsEnv, dir)

	t.Run("splitting", func(t *testing.T) {
		Satisfies(t, knownID)
		Observed(t, knownID, "a\tb\nc")
	})

	claims, err := ReadClaims(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"a b c"}
	if got := claims[knownID]; len(got) != 1 || !slices.Equal(got[0].Observations, want) {
		t.Errorf("observations = %+v, want %q", got, want)
	}
}
