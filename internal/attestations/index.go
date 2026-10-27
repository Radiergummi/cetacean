package attestations

import (
	"fmt"

	json "github.com/goccy/go-json"
)

const (
	predicateSPDX       = "https://spdx.dev/Document"
	predicateSLSAv02    = "https://slsa.dev/provenance/v0.2"
	predicateSLSAv1     = "https://slsa.dev/provenance/v1"
	annotationKind      = "vnd.docker.reference.type"
	annotationFor       = "vnd.docker.reference.digest"
	annotationPredicate = "in-toto.io/predicate-type"
)

var indexTypes = map[string]bool{
	"application/vnd.oci.image.index.v1+json":                   true,
	"application/vnd.docker.distribution.manifest.list.v2+json": true,
}

type descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations"`
	Platform    *struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant"`
	} `json:"platform"`
}

type manifest struct {
	Layers []Descriptor `json:"layers"`
}

func parseManifest(body []byte) (manifest, error) {
	var m manifest

	return m, json.Unmarshal(body, &m)
}

// parseIndex pairs each platform of an index with the attestation manifest
// BuildKit stored for it, reading those manifests through read. Anything but
// an index has no attestations, and gives nil.
func parseIndex(
	body []byte,
	mediaType string,
	read func(digest string) ([]byte, error),
) ([]Attested, error) {
	var index struct {
		MediaType string       `json:"mediaType"`
		Manifests []descriptor `json:"manifests"`
	}

	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}

	if !indexTypes[mediaType] && !indexTypes[index.MediaType] {
		return nil, nil
	}

	attestationFor := map[string]string{}

	for _, d := range index.Manifests {
		if d.Annotations[annotationKind] == "attestation-manifest" {
			attestationFor[d.Annotations[annotationFor]] = d.Digest
		}
	}

	var out []Attested

	for _, d := range index.Manifests {
		if d.Platform == nil || d.Annotations[annotationKind] == "attestation-manifest" {
			continue
		}

		a := Attested{
			Platform: Platform{
				OS:           d.Platform.OS,
				Architecture: d.Platform.Architecture,
				Variant:      d.Platform.Variant,
			},
			ManifestDigest: d.Digest,
		}

		if digest, ok := attestationFor[d.Digest]; ok {
			if err := readAttestation(&a, digest, read); err != nil {
				return nil, err
			}
		}

		out = append(out, a)
	}

	return out, nil
}

func readAttestation(a *Attested, digest string, read func(string) ([]byte, error)) error {
	body, err := read(digest)
	if err != nil {
		return err
	}

	var m struct {
		Layers []descriptor `json:"layers"`
	}

	if err := json.Unmarshal(body, &m); err != nil {
		return fmt.Errorf("attestation manifest %s: %w", digest, err)
	}

	for _, layer := range m.Layers {
		d := &Descriptor{Digest: layer.Digest, Size: layer.Size}

		switch layer.Annotations[annotationPredicate] {
		case predicateSLSAv02, predicateSLSAv1:
			a.Provenance = d
		case predicateSPDX:
			a.SBOM = d
		}
	}

	return nil
}
