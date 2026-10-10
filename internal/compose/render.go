package compose

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// header states what the document cannot carry, on every export. A reader who
// takes this file to another cluster needs to know before they try.
const header = `# Exported from Cetacean. Redeploys to the same state on the same cluster:
# secrets and configs are referenced, never exported, so another cluster needs
# them created first. This is not the file that originally created the stack.
`

// commentSafe spells out every rune that is not printable, line breaks among
// them, so a value a warning quotes can neither end its comment and start
// document content nor put a control character YAML refuses to load.
func commentSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\t' || unicode.IsPrint(r) {
			b.WriteRune(r)
		} else {
			fmt.Fprintf(&b, `\u%04X`, r)
		}
	}

	return b.String()
}

// Render writes the header, then any warnings as comments, then the document.
func Render(f File, warnings []string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(header)

	if len(warnings) > 0 {
		buf.WriteString("#\n# Not carried across:\n")
		for _, w := range warnings {
			buf.WriteString("# " + commentSafe(w) + "\n")
		}
	}

	buf.WriteString("\n")

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
