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
		slog.Warn(
			"sbom: could not project embedded CycloneDX; licenses page will be empty",
			"error",
			err,
		)
		doc = Document{Components: []Component{}}
	}

	// Only the served document carries text ids. Project stays a pure
	// projection of the SBOM so the generator that writes the text pool reads
	// its input free of the previous build's ids.
	attachTexts(doc.Components)

	if version.Date != "" && version.Date != "unknown" {
		doc.GeneratedAt = version.Date
	}

	// Document holds only strings and slices of structs, so marshaling cannot
	// fail; the error is discarded rather than handled with an unreachable path.
	projectedJSON, _ = json.Marshal(doc)
}

// Raw returns the embedded CycloneDX SBOM bytes.
func Raw() []byte { return raw }

// ProjectedJSON returns the flattened licenses Document as JSON, computed once
// at package initialization.
func ProjectedJSON() []byte { return projectedJSON }
