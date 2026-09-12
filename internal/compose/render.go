package compose

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// header states what the document cannot carry, on every export. A reader who
// takes this file to another cluster needs to know before they try.
const header = `# Exported from Cetacean. Redeploys to the same state on the same cluster:
# secrets and configs are referenced, never exported, so another cluster needs
# them created first. This is not the file that originally created the stack.
`

// Render writes the warning comment block and then the document. Warnings are
// a slice of strings by design — the honesty is the point, a severity model
// would be the speculative part.
func Render(f File, warnings []string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(header)

	if len(warnings) > 0 {
		buf.WriteString("#\n# Not carried across:\n")
		for _, w := range warnings {
			buf.WriteString("# " + w + "\n")
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
