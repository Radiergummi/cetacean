package attestations

import (
	"errors"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
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
		{"digest":"` + digestFor("amd") + `","platform":{"os":"linux","architecture":"amd64"}},
		{"digest":"` + digestFor("att") + `","platform":{"os":"unknown","architecture":"unknown"},
		 "annotations":{"vnd.docker.reference.type":"attestation-manifest",
		                "vnd.docker.reference.digest":"` + digestFor("amd") + `"}}]}`)
	attestation := []byte(`{"layers":[{"digest":"` + digestFor("bom") + `","size":10,
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

func TestParseIndexIgnoresMediaTypeParameters(t *testing.T) {
	spec.Satisfies(t, "oci/distribution-spec/client-ignores-content-type-parameters")

	// No mediaType field, so only the header can say this is an index.
	index := []byte(`{"manifests":[
		{"digest":"` + digestFor("amd") + `","platform":{"os":"linux","architecture":"amd64"}}]}`)
	read := func(string) ([]byte, error) { return nil, errors.New("not to be read") }

	for _, header := range []string{
		ociIndexType + "; charset=utf-8",
		"Application/VND.OCI.Image.Index.v1+JSON",
	} {
		if got, err := parseIndex(index, header, read); err != nil || len(got) != 1 {
			t.Errorf("%q: got %+v, %v; want the index read", header, got, err)
		}
	}
}

// Digests from an index become URL paths, so a malformed one is never read.
func TestParseIndexSkipsWhatIsNotADigest(t *testing.T) {
	const bad = "sha256:../../../other/manifests/latest"

	index := []byte(`{"mediaType":"` + ociIndexType + `","manifests":[
		{"digest":"` + bad + `","platform":{"os":"linux","architecture":"arm64"}},
		{"digest":"` + digestFor("amd") + `","platform":{"os":"linux","architecture":"amd64"}},
		{"digest":"` + bad + `","platform":{"os":"unknown","architecture":"unknown"},
		 "annotations":{"vnd.docker.reference.type":"attestation-manifest",
		                "vnd.docker.reference.digest":"` + digestFor("amd") + `"}}]}`)

	var read []string

	got, err := parseIndex(index, ociIndexType, func(digest string) ([]byte, error) {
		read = append(read, digest)

		return nil, errors.New("not to be read")
	})
	if err != nil || len(got) != 1 || got[0].ManifestDigest != digestFor("amd") {
		t.Errorf("got %+v, %v; want amd64 alone", got, err)
	}

	if len(read) != 0 {
		t.Errorf("read %v", read)
	}

	attestation := []byte(`{"layers":[{"digest":"` + bad + `","size":10,
		"annotations":{"in-toto.io/predicate-type":"https://spdx.dev/Document"}}]}`)

	var a Attested
	if err := readAttestation(&a, digestFor("att"), func(string) ([]byte, error) {
		return attestation, nil
	}); err != nil || a.SBOM != nil {
		t.Errorf("SBOM = %+v, %v; want a malformed layer digest skipped", a.SBOM, err)
	}
}
