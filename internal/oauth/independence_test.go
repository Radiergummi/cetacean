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

// The authorization server is a standalone concern, so no identifier, comment,
// log line or fixture in this package names its first consumer, MCP, with no
// exceptions. The needle is assembled at runtime: spelled as a literal, this
// test would fail on itself.
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

		// One pass over the whole file first, so the passing case — every case
		// in CI — never splits a line.
		scanned := strings.ToLower(string(body))
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
