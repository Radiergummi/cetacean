// Package sbom embeds the CycloneDX software bill of materials generated at
// build time (scripts/build-sbom.sh) and projects it into the flat licenses
// document served by the open-source licenses page.
package sbom

import (
	"bytes"
	"cmp"
	"slices"
	"strings"

	cyclonedx "github.com/CycloneDX/cyclonedx-go"
)

// Document is the projected licenses view served at GET /-/licenses.
type Document struct {
	GeneratedAt string      `json:"generatedAt,omitempty"`
	Components  []Component `json:"components"`
}

// Component is one open-source dependency bundled into Cetacean.
type Component struct {
	Name        string    `json:"name"`
	Version     string    `json:"version,omitempty"`
	Description string    `json:"description,omitempty"`
	Ecosystem   string    `json:"ecosystem"` // "go" | "npm" | "other"
	Licenses    []License `json:"licenses"`
	Homepage    string    `json:"homepage,omitempty"`
	Repository  string    `json:"repository,omitempty"`
}

// License is a single license entry for a component.
type License struct {
	ID   string `json:"id,omitempty"` // SPDX identifier or expression, when known
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// Project parses a CycloneDX JSON document and flattens it into a Document.
// Components lacking a recognized package URL (e.g. the synthetic container
// components a hierarchical merge produces) are skipped; nested components are
// walked recursively. Output is sorted by ecosystem, then name, so the
// projection is deterministic.
func Project(raw []byte) (Document, error) {
	var bom cyclonedx.BOM
	if err := cyclonedx.NewBOMDecoder(bytes.NewReader(raw), cyclonedx.BOMFileFormatJSON).
		Decode(&bom); err != nil {
		return Document{}, err
	}

	seen := make(map[string]bool)
	// Non-nil so an empty/edge-case BOM still marshals components as [] rather
	// than null; the API schema requires an array.
	components := []Component{}

	var walk func(in *[]cyclonedx.Component)
	walk = func(in *[]cyclonedx.Component) {
		if in == nil {
			return
		}

		for _, component := range *in {
			ecosystem := ecosystemFromPurl(component.PackageURL)
			// Key on ecosystem too: a Go module and an npm package can share a
			// name@version, and they are distinct components.
			key := ecosystem + ":" + component.Name + "@" + component.Version
			if ecosystem != "" && !seen[key] {
				seen[key] = true
				components = append(components, Component{
					Name:        component.Name,
					Version:     component.Version,
					Description: component.Description,
					Ecosystem:   ecosystem,
					Licenses:    projectLicenses(componentLicenses(component)),
					Homepage: externalReference(
						component.ExternalReferences,
						cyclonedx.ERTypeWebsite,
					),
					Repository: externalReference(
						component.ExternalReferences,
						cyclonedx.ERTypeVCS,
					),
				})
			}

			walk(component.Components)
		}
	}
	walk(bom.Components)

	slices.SortFunc(components, func(a, b Component) int {
		if a.Ecosystem != b.Ecosystem {
			return cmp.Compare(a.Ecosystem, b.Ecosystem)
		}

		if byName := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); byName != 0 {
			return byName
		}

		// Same ecosystem and name: fall back to version so components that share
		// a name (distinct versions) get a stable, total ordering.
		return cmp.Compare(a.Version, b.Version)
	})

	return Document{Components: components}, nil
}

func ecosystemFromPurl(purl string) string {
	switch {
	case purl == "":
		// No package URL — a synthetic container/application component, not a
		// dependency. Skip it.
		return ""
	case strings.HasPrefix(purl, "pkg:golang/"):
		return "go"
	case strings.HasPrefix(purl, "pkg:npm/"):
		return "npm"
	default:
		// A package with a purl of some other type (pkg:cargo/, pkg:pypi/, …).
		return "other"
	}
}

// componentLicenses returns the component's declared licenses, falling back to
// evidence.licenses when the top-level field is absent. cyclonedx-gomod records
// detected Go module licenses under component.evidence.licenses rather than
// component.licenses, so without this the Go modules would show no license.
func componentLicenses(component cyclonedx.Component) *cyclonedx.Licenses {
	if component.Licenses != nil && len(*component.Licenses) > 0 {
		return component.Licenses
	}

	if component.Evidence != nil {
		return component.Evidence.Licenses
	}

	return component.Licenses
}

func projectLicenses(in *cyclonedx.Licenses) []License {
	// Always return a non-nil slice: the licenses field is a required array in
	// the API schema, so a nil slice (which marshals to null) would be invalid.
	out := []License{}
	if in == nil {
		return out
	}

	for _, choice := range *in {
		switch {
		case choice.License != nil:
			out = append(out, License{
				ID:   choice.License.ID,
				Name: choice.License.Name,
				URL:  choice.License.URL,
			})
		case choice.Expression != "":
			out = append(out, License{ID: choice.Expression})
		}
	}

	return out
}

func externalReference(
	refs *[]cyclonedx.ExternalReference,
	kind cyclonedx.ExternalReferenceType,
) string {
	if refs == nil {
		return ""
	}

	for _, ref := range *refs {
		if ref.Type == kind {
			return ref.URL
		}
	}

	return ""
}
