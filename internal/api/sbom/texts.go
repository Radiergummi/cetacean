package sbom

import (
	_ "embed"
	"encoding/json"
	"log/slog"
)

//go:embed licensetexts.json
var rawTexts []byte

// textArtifact is the decoded licensetexts.json. Its shape is declared in
// artifact.go, shared with the generator that writes it. Initialized at
// package load time (before any init() functions run) to avoid init-order bugs.
var textArtifact = decodeTextArtifact()

func decodeTextArtifact() Artifact {
	var artifact Artifact

	if err := json.Unmarshal(rawTexts, &artifact); err != nil {
		// The licenses page degrades to the identifier-only inventory it
		// served before texts existed rather than taking the process down.
		slog.Warn(
			"sbom: could not decode embedded license texts; "+
				"the licenses page will show no full texts",
			"error", err,
		)
	}

	if artifact.Texts == nil {
		artifact.Texts = map[string]string{}
	}

	if artifact.Components == nil {
		artifact.Components = map[string]ComponentTexts{}
	}

	return artifact
}

// Text returns the pooled license or notice text for an id.
func Text(id string) (string, bool) {
	text, ok := textArtifact.Texts[id]

	return text, ok
}

// Attach stamps each component with the ids of its license and notice text.
// Components whose ecosystem has no local package store (and so no harvested
// text) keep their inventory entry with empty ids.
func Attach(components []Component) {
	for i := range components {
		entry, ok := textArtifact.Components[ComponentKey(components[i])]
		if !ok {
			continue
		}

		components[i].TextID = entry.License
		components[i].NoticeID = entry.Notice
	}
}
