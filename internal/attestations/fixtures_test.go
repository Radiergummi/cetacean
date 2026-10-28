package attestations

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The probe image: an OCI layout built from testdata/probe.Dockerfile.
const (
	probeIndex       = "sha256:2832cf1499a81ad8e3c2c1972fb99229adaa944ac2e29874c005c2a85a6f987a"
	probeManifest    = "sha256:17995b076b722acede7f725e257121d564eba50434bd78a762713b11ac7d6f65"
	probeProvenance  = "sha256:50c58172e8bb92a8ccc3861af14ff31d7d768ee27b2a4442e58d6e2fc9086fcd"
	probeRevision    = "b589d17d80928d746098f430a94f867a9c9e0092"
	ociIndexType     = "application/vnd.oci.image.index.v1+json"
	ociManifestType  = "application/vnd.oci.image.manifest.v1+json"
	probeRepository  = "https://github.com/example/attest-probe.git"
	nginxRepository  = "https://github.com/nginx/docker-nginx.git"
	nginxCommit      = "6fabb4c960a02a1e576d2789974c45f4cd2aa854"
	syftRepository   = "https://github.com/docker/buildkit-syft-scanner.git"
	syftCommit       = "1c368712e124e90ee912ec8615afa78974ddad19"
	probeLayerSizes  = "4092319 2565002 5194 144808 110 105"
	probeTotalLayers = 6
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	return body
}

func probeBlob(t *testing.T, digest string) []byte {
	t.Helper()

	return fixture(t, filepath.Join("probe", "blobs", strings.Replace(digest, ":", "/", 1)))
}

// probeLayers is the probe image's layers, in manifest order.
func probeLayers(t *testing.T) []Descriptor {
	t.Helper()

	manifest, err := parseManifest(probeBlob(t, probeManifest))
	if err != nil {
		t.Fatal(err)
	}

	return manifest.Layers
}

// digestFor is a well-formed digest standing for name in a synthetic index.
func digestFor(name string) string {
	sum := sha256.Sum256([]byte(name))

	return "sha256:" + hex.EncodeToString(sum[:])
}
