package oauth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the one place the acronym may appear, so it names itself rather
// than being recognised by the scan below.
const selfName = "independence_test.go"

// Assembled rather than spelled, for the same reason as the needle below.
var legacyStateFile = "m" + "c" + "p" + "-tokens.json"

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

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == selfName || filepath.Ext(name) != ".go" {
			continue
		}

		body, err := os.ReadFile(name) // #nosec G304 -- a name from this package's own directory
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		for i, line := range strings.Split(string(body), "\n") {
			// The state file's former name is the one legitimate mention: the
			// migration path has to name the file it migrates from, and a test
			// covering that has to name it too.
			scanned := strings.ReplaceAll(strings.ToLower(line), legacyStateFile, "")

			if strings.Contains(scanned, needle) {
				t.Errorf(
					"%s:%d names the consumer: %s",
					name,
					i+1,
					strings.TrimSpace(line),
				)
			}
		}
	}
}
