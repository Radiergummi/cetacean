// Package sbom embeds the CycloneDX software bill of materials generated at
// build time (scripts/build-sbom.sh) and projects it into the flat licenses
// document served by the open-source licenses page.
package sbom

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
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

// Minimal CycloneDX subset needed for projection.

type cycloneDX struct {
	Components []cdxComponent `json:"components"`
}

type cdxComponent struct {
	Name               string         `json:"name"`
	Version            string         `json:"version"`
	Description        string         `json:"description"`
	Purl               string         `json:"purl"`
	Licenses           []cdxLicense   `json:"licenses"`
	ExternalReferences []cdxExtRef    `json:"externalReferences"`
	Components         []cdxComponent `json:"components"`
}

type cdxLicense struct {
	License    *cdxLicenseChoice `json:"license"`
	Expression string            `json:"expression"`
}

type cdxLicenseChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type cdxExtRef struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// Project parses a CycloneDX JSON document and flattens it into a Document.
// Components lacking a recognized package URL (e.g. the synthetic container
// components a hierarchical merge produces) are skipped; nested components are
// walked recursively. Output is sorted by ecosystem, then name, so the
// projection is deterministic.
func Project(raw []byte) (Document, error) {
	var cdx cycloneDX
	if err := json.Unmarshal(raw, &cdx); err != nil {
		return Document{}, err
	}

	seen := make(map[string]bool)
	var components []Component

	var walk func(in []cdxComponent)
	walk = func(in []cdxComponent) {
		for _, component := range in {
			ecosystem := ecosystemFromPurl(component.Purl)
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
					Licenses:    projectLicenses(component.Licenses),
					Homepage:    externalReference(component.ExternalReferences, "website"),
					Repository:  externalReference(component.ExternalReferences, "vcs"),
				})
			}

			walk(component.Components)
		}
	}
	walk(cdx.Components)

	slices.SortFunc(components, func(a, b Component) int {
		if a.Ecosystem != b.Ecosystem {
			return cmp.Compare(a.Ecosystem, b.Ecosystem)
		}

		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
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

func projectLicenses(in []cdxLicense) []License {
	out := make([]License, 0, len(in))
	for _, license := range in {
		switch {
		case license.License != nil:
			out = append(out, License{
				ID:   license.License.ID,
				Name: license.License.Name,
				URL:  license.License.URL,
			})
		case license.Expression != "":
			out = append(out, License{ID: license.Expression})
		}
	}

	return out
}

func externalReference(refs []cdxExtRef, kind string) string {
	for _, ref := range refs {
		if ref.Type == kind {
			return ref.URL
		}
	}

	return ""
}
