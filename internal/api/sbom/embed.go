package sbom

import (
	_ "embed"
	"encoding/json"
	"log/slog"

	"github.com/radiergummi/cetacean/internal/version"
)

//go:embed sbom.cdx.json
var raw []byte

var projectedJSON []byte

func init() {
	doc, err := Project(raw)
	if err != nil {
		slog.Warn("sbom: could not project embedded CycloneDX; licenses page will be empty", "error", err)
		doc = Document{Components: []Component{}}
	}

	if version.Date != "" && version.Date != "unknown" {
		doc.GeneratedAt = version.Date
	}

	body, err := json.Marshal(doc)
	if err != nil {
		slog.Warn("sbom: could not marshal projected document", "error", err)
		body = []byte(`{"components":[]}`)
	}
	projectedJSON = body
}

// Raw returns the embedded CycloneDX SBOM bytes.
func Raw() []byte { return raw }

// ProjectedJSON returns the flattened licenses Document as JSON, computed once
// at package initialization.
func ProjectedJSON() []byte { return projectedJSON }
