package attestations

import (
	"bytes"
	"maps"
	"slices"
	"testing"
)

func parsedSBOM(t *testing.T, statement []byte) *SBOM {
	t.Helper()

	sbom, _, err := ParseSBOM(statement)
	if err != nil {
		t.Fatalf("ParseSBOM: %v", err)
	}

	return sbom
}

func packageNamed(sbom *SBOM, name string) *Package {
	for i := range sbom.Packages {
		if sbom.Packages[i].Name == name {
			return &sbom.Packages[i]
		}
	}

	return nil
}

func TestSBOMNginx(t *testing.T) {
	sbom := parsedSBOM(t, fixture(t, "nginx-sbom.json"))

	if packageNamed(sbom, "sbom") != nil {
		t.Error("the described root package was listed")
	}

	if want := map[string]int{"deb": 4, "maven": 1}; !maps.Equal(sbom.Ecosystems, want) {
		t.Errorf("ecosystems = %v, want %v", sbom.Ecosystems, want)
	}

	acl := packageNamed(sbom, "acl")
	if acl == nil ||
		acl.License != "GPL-2.0-only OR GPL-2.0-or-later OR LGPL-2.0-or-later OR LGPL-2.1-only" {
		t.Errorf("acl = %+v; a NOASSERTION conclusion falls back to the declared license", acl)
	}

	if keyring := packageNamed(
		sbom,
		"debian-archive-keyring",
	); keyring == nil ||
		keyring.License != "" {
		t.Errorf("keyring = %+v; no license means empty", keyring)
	}

	if sbom.Licensed != 3 {
		t.Errorf("licensed = %d, want 3", sbom.Licensed)
	}

	if !slices.Contains(sbom.Generators, "Tool: docker-scout-1.18.1") || sbom.Created == nil {
		t.Errorf("generators %v, created %v", sbom.Generators, sbom.Created)
	}

	names := make([]string, len(sbom.Packages))
	for i, p := range sbom.Packages {
		names[i] = p.Name
	}

	if !slices.IsSorted(names) {
		t.Errorf("packages not sorted: %v", names)
	}
}

func TestSBOMSyftFoundIn(t *testing.T) {
	sbom := parsedSBOM(t, fixture(t, "syft-sbom.json"))

	if len(sbom.Packages) != 6 {
		t.Fatalf("%d packages, want 6 (the directory root is described)", len(sbom.Packages))
	}

	p := packageNamed(sbom, "cel.dev/expr")
	if p == nil || p.FoundIn != "go module information: /bin/syft-scanner" ||
		p.Ecosystem != "golang" || p.Version != "v0.25.1" || p.License != "" {
		t.Errorf("cel.dev/expr = %+v", p)
	}
}

func TestSBOMHonoursDocumentDescribes(t *testing.T) {
	sbom := parsedSBOM(t, []byte(`{"predicateType":"https://spdx.dev/Document","predicate":{
		"documentDescribes":["SPDXRef-root"],
		"packages":[{"SPDXID":"SPDXRef-root","name":"image"},
		            {"SPDXID":"SPDXRef-a","name":"a","versionInfo":"1"}]}}`))

	if len(sbom.Packages) != 1 || sbom.Packages[0].Name != "a" {
		t.Errorf("packages = %+v", sbom.Packages)
	}
}

func TestSBOMUnknownEcosystem(t *testing.T) {
	sbom := parsedSBOM(t, []byte(`{"predicateType":"https://spdx.dev/Document","predicate":{
		"packages":[{"SPDXID":"SPDXRef-a","name":"a","licenseConcluded":"NONE"}]}}`))

	if p := sbom.Packages[0]; p.Ecosystem != "unknown" || p.License != "" {
		t.Errorf("package = %+v", p)
	}

	if sbom.Ecosystems["unknown"] != 1 {
		t.Errorf("ecosystems = %v", sbom.Ecosystems)
	}
}

func TestSBOMReturnsTheDocumentAsStored(t *testing.T) {
	statement := fixture(t, "syft-sbom.json")

	_, raw, err := ParseSBOM(statement)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) ||
		!bytes.Contains(
			raw,
			[]byte(`"spdxVersion"`),
		) || bytes.Contains(raw, []byte(`"predicateType"`)) {
		t.Errorf("raw is not the bare SPDX document: %.80s", raw)
	}
}

func TestSBOMRefusesOtherPredicates(t *testing.T) {
	if _, _, err := ParseSBOM(fixture(t, "syft-provenance.json")); err == nil {
		t.Error("provenance was read as an SBOM")
	}
}
