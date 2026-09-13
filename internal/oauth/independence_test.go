package oauth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the one place the acronym may appear, so it names itself rather
// than being recognised by the scan below. Both are assembled rather than
// spelled, for the same reason as the needle.
const (
	selfName        = "independence_test.go"
	legacyStateFile = "m" + "c" + "p" + "-tokens.json"
)

// The authorization server is a standalone concern: MCP is one consumer of it,
// and a second protected resource is planned. A mention of the first consumer
// anywhere in here — an identifier, a comment, a log line, a fixture — is the
// beginning of the coupling this package was extracted to remove, so the rule
// is enforced rather than remembered.
//
// The needle is assembled at runtime: spelled as a literal, this test would
// fail on itself.
func TestThePackageDoesNotNameItsConsumer(t *testing.T) {
	needle := "m" + "c" + "p"

	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list package sources: %v", err)
	}

	for _, name := range names {
		if name == selfName {
			continue
		}

		body, err := os.ReadFile(name) // #nosec G304 -- a name from this package's own directory
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		// The state file's former name is the one legitimate mention: the
		// migration path has to name the file it migrates from, and a test
		// covering that has to name it too. Erased once per file rather than
		// once per line, so the passing case — every case in CI — is one pass.
		scanned := strings.ReplaceAll(strings.ToLower(string(body)), legacyStateFile, "")
		if !strings.Contains(scanned, needle) {
			continue
		}

		// Only now is a line-by-line pass worth it, to name the offender.
		for i, line := range strings.Split(scanned, "\n") {
			if strings.Contains(line, needle) {
				t.Errorf("%s:%d names the consumer: %s", name, i+1, strings.TrimSpace(line))
			}
		}
	}
}
