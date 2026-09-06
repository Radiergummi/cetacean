package api

import (
	"os"
	"regexp"
	"testing"
)

// The dashboard carries its own dictionary of friendly titles and suggestions,
// keyed by the codes this registry emits (frontend/src/lib/errors.ts). It is a
// curated subset — a code with no entry there falls back to the generic
// message, which is fine — but an entry keyed on a code the backend can no
// longer emit is dead text nobody will notice, since the fallback looks the
// same. Renaming or dropping a code here has to fail there.
func TestFrontendErrorDictionaryOnlyNamesCodesWeEmit(t *testing.T) {
	const path = "../../frontend/src/lib/errors.ts"

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	// Dictionary keys are the only two-space-indented `ABC123:` lines.
	keyPattern := regexp.MustCompile(`(?m)^ {2}([A-Z]{3}\d{3}):`)
	matches := keyPattern.FindAllSubmatch(source, -1)

	if len(matches) == 0 {
		t.Fatalf("no error codes found in %s — has its shape changed?", path)
	}

	for _, match := range matches {
		code := string(match[1])
		if _, ok := errorRegistry[code]; !ok {
			t.Errorf(
				"%s maps %s, which no longer exists in errorRegistry — "+
					"remove or repoint the dashboard entry",
				path, code,
			)
		}
	}
}
