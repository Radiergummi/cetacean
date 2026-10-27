package attestations

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	json "github.com/goccy/go-json"
)

// Syft writes this before the cataloguer and paths a package was found by.
const sourceInfoPrefix = "acquired package info from "

type spdxDocument struct {
	CreationInfo struct {
		Created  string   `json:"created"`
		Creators []string `json:"creators"`
	} `json:"creationInfo"`
	DocumentDescribes []string `json:"documentDescribes"`
	Packages          []struct {
		SPDXID           string `json:"SPDXID"`
		Name             string `json:"name"`
		VersionInfo      string `json:"versionInfo"`
		LicenseConcluded string `json:"licenseConcluded"`
		LicenseDeclared  string `json:"licenseDeclared"`
		SourceInfo       string `json:"sourceInfo"`
		ExternalRefs     []struct {
			ReferenceType    string `json:"referenceType"`
			ReferenceLocator string `json:"referenceLocator"`
		} `json:"externalRefs"`
	} `json:"packages"`
	Relationships []struct {
		Element string `json:"spdxElementId"`
		Type    string `json:"relationshipType"`
		Related string `json:"relatedSpdxElement"`
	} `json:"relationships"`
}

// ParseSBOM reads an SPDX statement into its package table, and returns the
// SPDX document itself as the statement held it.
func ParseSBOM(body []byte) (*SBOM, []byte, error) {
	var st statement
	if err := json.Unmarshal(body, &st); err != nil {
		return nil, nil, fmt.Errorf("statement: %w", err)
	}

	if st.PredicateType != predicateSPDX {
		return nil, nil, fmt.Errorf("predicate %q is not an SPDX document", st.PredicateType)
	}

	var doc spdxDocument
	if err := json.Unmarshal(st.Predicate, &doc); err != nil {
		return nil, nil, fmt.Errorf("SPDX document: %w", err)
	}

	// The packages standing for the image itself are not in it.
	described := map[string]bool{}
	for _, id := range doc.DocumentDescribes {
		described[id] = true
	}

	for _, r := range doc.Relationships {
		if r.Element == "SPDXRef-DOCUMENT" && r.Type == "DESCRIBES" {
			described[r.Related] = true
		}
	}

	sbom := &SBOM{
		Generators: doc.CreationInfo.Creators,
		Created:    parseTime(doc.CreationInfo.Created),
		Ecosystems: map[string]int{},
	}

	for _, p := range doc.Packages {
		if described[p.SPDXID] {
			continue
		}

		pkg := Package{
			Name:      p.Name,
			Version:   p.VersionInfo,
			Ecosystem: "unknown",
			License:   license(p.LicenseConcluded),
			FoundIn:   strings.TrimPrefix(p.SourceInfo, sourceInfoPrefix),
		}

		if pkg.License == "" {
			pkg.License = license(p.LicenseDeclared)
		}

		for _, ref := range p.ExternalRefs {
			if ref.ReferenceType == "purl" {
				pkg.PURL = ref.ReferenceLocator
				if kind, _, ok := strings.Cut(strings.TrimPrefix(pkg.PURL, "pkg:"), "/"); ok {
					pkg.Ecosystem = kind
				}
			}
		}

		sbom.Ecosystems[pkg.Ecosystem]++
		if pkg.License != "" {
			sbom.Licensed++
		}

		sbom.Packages = append(sbom.Packages, pkg)
	}

	slices.SortFunc(sbom.Packages, func(a, b Package) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Version, b.Version))
	})

	return sbom, st.Predicate, nil
}

func license(value string) string {
	if value == "NOASSERTION" || value == "NONE" {
		return ""
	}

	return value
}
