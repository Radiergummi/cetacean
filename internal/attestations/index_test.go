package attestations

import (
	"errors"
	"testing"
)

func TestParseIndexPairsAttestationsWithPlatforms(t *testing.T) {
	read := func(digest string) ([]byte, error) { return probeBlob(t, digest), nil }

	got, err := parseIndex(probeBlob(t, probeIndex), ociIndexType, read)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d platforms, want 1: %+v", len(got), got)
	}

	p := got[0]
	if p.Platform != (Platform{OS: "linux", Architecture: "arm64"}) {
		t.Errorf("platform = %+v", p.Platform)
	}

	if p.ManifestDigest != probeManifest {
		t.Errorf("manifest = %s", p.ManifestDigest)
	}

	if p.Provenance == nil || p.Provenance.Digest != probeProvenance {
		t.Errorf("provenance = %+v", p.Provenance)
	}

	if p.SBOM != nil {
		t.Errorf("the probe was built without an SBOM, got %+v", p.SBOM)
	}
}

func TestParseIndexPlainManifestIsNone(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, errors.New("not to be read") }

	got, err := parseIndex(probeBlob(t, probeManifest), ociManifestType, read)
	if err != nil || got != nil {
		t.Errorf("got %+v, %v; want nothing", got, err)
	}
}

func TestParseIndexIgnoresUnknownPredicates(t *testing.T) {
	index := []byte(`{"mediaType":"` + ociIndexType + `","manifests":[
		{"digest":"sha256:aa","platform":{"os":"linux","architecture":"amd64"}},
		{"digest":"sha256:bb","platform":{"os":"unknown","architecture":"unknown"},
		 "annotations":{"vnd.docker.reference.type":"attestation-manifest",
		                "vnd.docker.reference.digest":"sha256:aa"}}]}`)
	attestation := []byte(`{"layers":[{"digest":"sha256:cc","size":10,
		"annotations":{"in-toto.io/predicate-type":"https://cyclonedx.org/bom"}}]}`)

	got, err := parseIndex(index, ociIndexType, func(string) ([]byte, error) {
		return attestation, nil
	})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}

	if got[0].SBOM != nil || got[0].Provenance != nil {
		t.Errorf("a CycloneDX layer was taken for something: %+v", got[0])
	}
}
