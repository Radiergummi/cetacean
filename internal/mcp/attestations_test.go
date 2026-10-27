package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/attestations"
	"github.com/radiergummi/cetacean/internal/cache"
)

const attestedImage = "app:1@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type fakeAttestationSource struct {
	calls int
}

func (f *fakeAttestationSource) Image(context.Context, string) attestations.Image {
	f.calls++

	return attestations.Image{
		Status: attestations.StatusAvailable,
		Platforms: []attestations.ImagePlatform{
			{
				Platform:       attestations.Platform{OS: "linux", Architecture: "amd64"},
				ManifestDigest: "sha256:amd",
			},
			{
				Platform:       attestations.Platform{OS: "linux", Architecture: "arm64"},
				ManifestDigest: "sha256:arm",
			},
		},
	}
}

func (f *fakeAttestationSource) SBOM(
	_ context.Context,
	_ string,
	p attestations.Platform,
) (*attestations.SBOM, []byte, error) {
	f.calls++

	return &attestations.SBOM{
		Packages:   []attestations.Package{{Name: "pkg-" + p.Architecture, Ecosystem: "golang"}},
		Ecosystems: map[string]int{"golang": 1},
	}, []byte(`{"spdxVersion":"SPDX-2.3"}`), nil
}

func (f *fakeAttestationSource) Dockerfile(
	context.Context,
	string,
	attestations.Platform,
) (*attestations.Dockerfile, error) {
	f.calls++

	return nil, errors.New("unused")
}

func attestationCache() *cache.Cache {
	c := cache.New(nil)
	spec := swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: attestedImage}}

	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}, TaskTemplate: spec},
	})
	c.SetNode(swarm.Node{
		ID: "node1",
		Description: swarm.NodeDescription{
			Platform: swarm.Platform{OS: "linux", Architecture: "aarch64"},
		},
	})
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1", NodeID: "node1", Spec: spec})

	return c
}

func TestProvenanceResourceACL(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:other"))

	src := &fakeAttestationSource{}
	srv := newResourceTestServer(t, attestationCache(), func(o *Options) {
		o.ACL = e
		o.Attestations = src
	})

	for _, uri := range []string{
		"cetacean://services/svc1/provenance",
		"cetacean://services/svc1/sbom?platform=linux/amd64",
		"cetacean://tasks/task1/provenance",
	} {
		if _, err := srv.readResource(ctxWithIdentity(), uri); err == nil {
			t.Errorf("%s: read without access", uri)
		}
	}

	if src.calls != 0 {
		t.Errorf("the source was asked %d times for what the caller cannot read", src.calls)
	}
}

func TestProvenanceResource(t *testing.T) {
	srv := newResourceTestServer(t, attestationCache(), func(o *Options) {
		o.Attestations = &fakeAttestationSource{}
	})

	body, err := srv.readResource(context.Background(), "cetacean://tasks/task1/provenance")
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Status    string `json:"status"`
		Platforms []struct {
			Platform string `json:"platform"`
		} `json:"platforms"`
	}

	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}

	if got.Status != "available" || len(got.Platforms) != 1 ||
		got.Platforms[0].Platform != "linux/arm64" {
		t.Errorf("got %+v", got)
	}
}

func TestSBOMResourceIsTable(t *testing.T) {
	srv := newResourceTestServer(t, attestationCache(), func(o *Options) {
		o.Attestations = &fakeAttestationSource{}
	})

	body, mime, err := srv.readResourceContents(context.Background(),
		"cetacean://services/svc1/sbom?platform=linux/amd64")
	if err != nil {
		t.Fatal(err)
	}

	if mime != mcpMIMEType || !strings.Contains(body, `"packages"`) ||
		!strings.Contains(body, "pkg-amd64") || strings.Contains(body, "spdxVersion") {
		t.Errorf("%s: %s", mime, body)
	}
}

func TestServiceResourceDoesNotFetch(t *testing.T) {
	src := &fakeAttestationSource{}
	srv := newResourceTestServer(t, attestationCache(), func(o *Options) { o.Attestations = src })

	if _, err := srv.readResource(context.Background(), "cetacean://services/svc1"); err != nil {
		t.Fatal(err)
	}

	if src.calls != 0 {
		t.Errorf("reading the service asked the registry %d times", src.calls)
	}
}

func TestSBOMResourcePlatformRequired(t *testing.T) {
	srv := newResourceTestServer(t, attestationCache(), func(o *Options) {
		o.Attestations = &fakeAttestationSource{}
	})

	_, err := srv.readResource(context.Background(), "cetacean://services/svc1/sbom")
	if err == nil || !strings.Contains(err.Error(), "linux/arm64") {
		t.Errorf("err = %v; want the candidates named", err)
	}
}
