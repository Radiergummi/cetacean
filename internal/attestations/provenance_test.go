package attestations

import (
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

func parsedProvenance(t *testing.T, statement []byte) *Provenance {
	t.Helper()

	p, err := ParseProvenance(statement)
	if err != nil {
		t.Fatalf("ParseProvenance: %v", err)
	}

	return p
}

func TestProvenanceV02Nginx(t *testing.T) {
	p := parsedProvenance(t, fixture(t, "nginx-provenance.json"))

	want := Source{
		Repository:   nginxRepository,
		Ref:          nginxCommit,
		Commit:       nginxCommit,
		Subdirectory: "mainline/debian",
	}
	if p.Source == nil || *p.Source != want {
		t.Errorf("source = %+v, want %+v", p.Source, want)
	}

	if p.BuilderID != "https://github.com/docker-library" || p.DockerfilePath != "Dockerfile" {
		t.Errorf("builder %q, Dockerfile %q", p.BuilderID, p.DockerfilePath)
	}

	if p.Frontend != "gateway.v0" {
		t.Errorf("frontend = %q", p.Frontend)
	}

	if p.Reproducible == nil || *p.Reproducible {
		t.Errorf("reproducible = %v, want false", p.Reproducible)
	}

	if p.StartedOn == nil || p.FinishedOn == nil {
		t.Error("build times missing")
	}

	if !p.HasSource {
		t.Error("nginx's v0.2 provenance embeds its Dockerfile")
	}

	var staging *Input

	for i, in := range p.Inputs {
		if strings.HasPrefix(in.Image, "oisupport/staging-amd64:") {
			staging = &p.Inputs[i]
		}
	}

	if staging == nil || !strings.HasPrefix(
		staging.Image,
		"oisupport/staging-amd64:65db3a615990481c0625b159a0f93cc2918929ef4247146d715f70ade5339d1e@sha256:4876fdea",
	) {
		t.Errorf("staging input = %+v", staging)
	}

	if staging != nil && staging.Digest == "" {
		t.Error("an input carries its digest")
	}
}

func TestProvenanceV1Syft(t *testing.T) {
	p := parsedProvenance(t, fixture(t, "syft-provenance.json"))

	want := Source{Repository: syftRepository, Ref: "refs/tags/v1.12.1", Commit: syftCommit}
	if p.Source == nil || *p.Source != want {
		t.Errorf("source = %+v, want %+v", p.Source, want)
	}

	if !strings.HasSuffix(p.BuilderID, "/actions/runs/40359464897/attempts/1") {
		t.Errorf("builder = %q", p.BuilderID)
	}

	if p.CI == nil {
		t.Fatal("no CI context")
	}

	ci := *p.CI
	if ci.Actor != "crazy-max" || ci.Event != "push" || ci.RunID != "40359464897" ||
		ci.RunAttempt != "1" || ci.RunnerEnvironment != "github-hosted" ||
		ci.Ref != "refs/tags/v1.12.1" || ci.ServerURL != "https://github.com" ||
		ci.Repository != "docker/buildkit-syft-scanner" ||
		ci.WorkflowRef != "docker/buildkit-syft-scanner/.github/workflows/ci.yaml@refs/tags/v1.12.1" ||
		ci.WorkflowSHA != "422dfad83311be8c7e03e219ab6e8f70f4619575" {
		t.Errorf("CI = %+v", ci)
	}

	if !p.HasSource {
		t.Error("mode=max provenance embeds its source")
	}
}

func TestProvenanceVCSFallback(t *testing.T) {
	p := parsedProvenance(t, probeBlob(t, probeProvenance))

	want := Source{Repository: probeRepository, Commit: probeRevision}
	if p.Source == nil || *p.Source != want {
		t.Errorf("source = %+v, want %+v", p.Source, want)
	}

	if p.DockerfilePath != "Dockerfile" {
		t.Errorf("Dockerfile = %q", p.DockerfilePath)
	}
}

func TestProvenanceV02HasNoCI(t *testing.T) {
	if p := parsedProvenance(t, fixture(t, "nginx-provenance.json")); p.CI != nil {
		t.Errorf("CI = %+v", p.CI)
	}
}

func TestProvenanceWithoutPredicateIsEmpty(t *testing.T) {
	spec.Satisfies(t, "intoto/statement-v1/predicate-may-be-omitted")

	p := parsedProvenance(t, []byte(`{"_type":"https://in-toto.io/Statement/v1",
		"predicateType":"https://slsa.dev/provenance/v1","subject":[]}`))
	if p.Source != nil || p.BuilderID != "" || p.HasSource {
		t.Errorf("an omitted predicate read as %+v", p)
	}
}

func TestProvenanceV1IgnoresUnknownFieldsAndEmptyValues(t *testing.T) {
	spec.Satisfies(t,
		"slsa/provenance-v1/consumers-ignore-unrecognized-fields",
		"slsa/provenance-v1/unset-null-empty-equivalent",
	)

	p := parsedProvenance(t, []byte(`{"predicateType":"https://slsa.dev/provenance/v1",
		"predicate":{"vendor_extra":{"x":1},
		"buildDefinition":{"externalParameters":null,"resolvedDependencies":[]},
		"runDetails":{"builder":{"id":""},"metadata":{"startedOn":"","finishedOn":null}}}}`))
	if p.BuilderID != "" || p.StartedOn != nil || p.FinishedOn != nil || p.Source != nil ||
		len(p.Inputs) != 0 {
		t.Errorf("empty and null values read as %+v", p)
	}
}

func TestProvenanceV02IgnoresUnknownFieldsAndNulls(t *testing.T) {
	spec.Satisfies(t,
		"slsa/provenance-v0.2/consumers-ignore-unrecognized-fields",
		"slsa/provenance-v0.2/unset-and-null-equivalent",
	)

	p := parsedProvenance(t, []byte(`{"predicateType":"https://slsa.dev/provenance/v0.2",
		"predicate":{"https://example.com/ext":true,"builder":null,"invocation":null,
		"materials":null,"metadata":{"buildStartedOn":null}}}`))
	if p.BuilderID != "" || p.StartedOn != nil || p.Source != nil || len(p.Inputs) != 0 {
		t.Errorf("null values read as %+v", p)
	}
}

func TestProvenanceRefusesOtherPredicates(t *testing.T) {
	if _, err := ParseProvenance(
		[]byte(`{"predicateType":"https://spdx.dev/Document"}`),
	); err == nil {
		t.Error("an SBOM was read as provenance")
	}
}
