package compose

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// header states what the document cannot carry, on every export. A reader who
// takes this file to another cluster needs to know before they try.
const header = `# Exported from Cetacean. Redeploys to the same state on the same cluster:
# secrets and configs are referenced, never exported, so another cluster needs
# them created first. This is not the file that originally created the stack.
`

// commentSafe spells out every YAML line break, so a value quoted in a warning
// cannot end its comment line and start document content.
var commentSafe = strings.NewReplacer(
	"\r", `\r`, "\n", `\n`, "\u0085", `\u0085`, "\u2028", `\u2028`, "\u2029", `\u2029`,
)

// Render writes the header, then any warnings as comments, then the document.
func Render(f File, warnings []string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(header)

	if len(warnings) > 0 {
		buf.WriteString("#\n# Not carried across:\n")
		for _, w := range warnings {
			buf.WriteString("# " + commentSafe.Replace(w) + "\n")
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
