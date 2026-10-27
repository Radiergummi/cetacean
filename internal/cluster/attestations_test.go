package cluster

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/attestations"
)

var (
	amd64 = attestations.Platform{OS: "linux", Architecture: "amd64"}
	arm64 = attestations.Platform{OS: "linux", Architecture: "arm64"}
	armv6 = attestations.Platform{OS: "linux", Architecture: "arm", Variant: "v6"}
	armv7 = attestations.Platform{OS: "linux", Architecture: "arm", Variant: "v7"}
)

type fakeAttestations struct {
	image attestations.Image
	calls int
}

func (f *fakeAttestations) Image(context.Context, string) attestations.Image {
	f.calls++

	return f.image
}

func (f *fakeAttestations) SBOM(
	context.Context,
	string,
	attestations.Platform,
) (*attestations.SBOM, []byte, error) {
	return nil, nil, errors.New("unused")
}

func (f *fakeAttestations) Dockerfile(
	context.Context,
	string,
	attestations.Platform,
) (*attestations.Dockerfile, error) {
	return nil, errors.New("unused")
}

func imageWith(platforms ...attestations.Platform) attestations.Image {
	img := attestations.Image{Status: attestations.StatusAvailable}
	for _, p := range platforms {
		img.Platforms = append(img.Platforms, attestations.ImagePlatform{
			Platform:       p,
			ManifestDigest: "sha256:" + p.String(),
		})
	}

	return img
}

func nodeOn(id, arch string) swarm.Node {
	return swarm.Node{
		ID: id,
		Description: swarm.NodeDescription{
			Platform: swarm.Platform{OS: "linux", Architecture: arch},
		},
	}
}

func taskOn(nodeID string, state swarm.TaskState) swarm.Task {
	return swarm.Task{
		NodeID: nodeID,
		Status: swarm.TaskStatus{State: state},
		Spec: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{
			Image: "nginx:1@sha256:" + "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90",
		}},
	}
}

func TestTaskPlatformsNormalisesNodeArch(t *testing.T) {
	node := nodeOn("n1", "aarch64")

	got := TaskPlatforms(taskOn("n1", swarm.TaskStateRunning), &node, imageWith(amd64, arm64))
	if !slices.Equal(got, []attestations.Platform{arm64}) {
		t.Errorf("got %v", got)
	}
}

func TestTaskPlatformsArmVariantsAreAllCandidates(t *testing.T) {
	node := nodeOn("n1", "armv7l")

	got := TaskPlatforms(
		taskOn("n1", swarm.TaskStateRunning),
		&node,
		imageWith(amd64, armv6, armv7),
	)
	if !slices.Equal(got, []attestations.Platform{armv6, armv7}) {
		t.Errorf("got %v", got)
	}
}

func TestTaskPlatformsUnplacedGetsAll(t *testing.T) {
	got := TaskPlatforms(taskOn("", swarm.TaskStatePending), nil, imageWith(amd64, arm64))
	if !slices.Equal(got, []attestations.Platform{amd64, arm64}) {
		t.Errorf("got %v", got)
	}
}

func TestSelectPlatform(t *testing.T) {
	if p, err := SelectPlatform([]attestations.Platform{amd64}, ""); err != nil || p != amd64 {
		t.Errorf("one candidate: %v, %v", p, err)
	}

	if _, err := SelectPlatform([]attestations.Platform{amd64, arm64}, ""); !errors.Is(
		err, ErrPlatformRequired) {
		t.Errorf("two candidates, none named: %v", err)
	}

	if p, err := SelectPlatform([]attestations.Platform{amd64, arm64}, "linux/arm64"); err != nil ||
		p != arm64 {
		t.Errorf("named: %v, %v", p, err)
	}

	if _, err := SelectPlatform([]attestations.Platform{amd64}, "linux/s390x"); !errors.Is(
		err, ErrPlatformUnknown) {
		t.Errorf("unknown: %v", err)
	}
}

func TestServiceProvenanceCountsRunningTasks(t *testing.T) {
	nodes := map[string]swarm.Node{
		"a": nodeOn("a", "x86_64"), "b": nodeOn("b", "x86_64"), "c": nodeOn("c", "aarch64"),
	}
	tasks := []swarm.Task{
		taskOn("a", swarm.TaskStateRunning), taskOn("a", swarm.TaskStateRunning),
		taskOn("b", swarm.TaskStateRunning), taskOn("c", swarm.TaskStateRunning),
		taskOn("b", swarm.TaskStateShutdown),
	}
	svc := swarm.Service{Spec: swarm.ServiceSpec{TaskTemplate: tasks[0].Spec}}
	src := &fakeAttestations{image: imageWith(amd64, arm64)}

	got := ServiceProvenance(context.Background(), svc, tasks, func(id string) (swarm.Node, bool) {
		n, ok := nodes[id]

		return n, ok
	}, src)

	if got.Status != attestations.StatusAvailable || len(got.Platforms) != 2 {
		t.Fatalf("got %+v", got)
	}

	if got.Platforms[0].RunningTasks != 3 || got.Platforms[1].RunningTasks != 1 {
		t.Errorf("running = %d, %d; want 3, 1",
			got.Platforms[0].RunningTasks, got.Platforms[1].RunningTasks)
	}
}

func TestTaskProvenanceShowsItsOwnPlatform(t *testing.T) {
	node := nodeOn("n1", "aarch64")
	src := &fakeAttestations{image: imageWith(amd64, arm64)}

	got := TaskProvenance(context.Background(), taskOn("n1", swarm.TaskStateRunning), &node, src)
	if len(got.Platforms) != 1 || got.Platforms[0].Platform != "linux/arm64" {
		t.Errorf("platforms = %+v", got.Platforms)
	}
}

func summaryOf(p *attestations.Provenance) *ProvenanceSummary {
	img := imageWith(amd64)
	img.Platforms[0].Provenance = p

	got := ServiceProvenance(context.Background(), swarm.Service{Spec: swarm.ServiceSpec{
		TaskTemplate: taskOn("", "").Spec,
	}}, nil, nil, &fakeAttestations{image: img})

	return got.Platforms[0].Provenance
}

func TestSourceLinks(t *testing.T) {
	const sha = "1c368712e124e90ee912ec8615afa78974ddad19"

	cases := []struct {
		repository, repositoryURL, commitURL string
	}{
		{
			"https://github.com/docker/buildkit-syft-scanner.git",
			"https://github.com/docker/buildkit-syft-scanner",
			"https://github.com/docker/buildkit-syft-scanner/commit/" + sha,
		},
		{
			"https://gitlab.com/group/project.git",
			"https://gitlab.com/group/project",
			"https://gitlab.com/group/project/-/commit/" + sha,
		},
		{
			"https://codeberg.org/org/repo",
			"https://codeberg.org/org/repo",
			"https://codeberg.org/org/repo/commit/" + sha,
		},
		{"https://git.example.com/org/repo.git", "", ""},
	}

	for _, tc := range cases {
		s := summaryOf(&attestations.Provenance{
			Source: &attestations.Source{Repository: tc.repository, Commit: sha},
		}).Source
		if s.RepositoryURL != tc.repositoryURL || s.CommitURL != tc.commitURL {
			t.Errorf("%s: links %q, %q", tc.repository, s.RepositoryURL, s.CommitURL)
		}
	}
}

func TestSourceStripsUserinfo(t *testing.T) {
	s := summaryOf(&attestations.Provenance{
		Source: &attestations.Source{ //nolint:gosec // G101: fake credentials, stripped in the test.
			Repository: "https://user:tok@github.com/a/b.git",
			Commit:     "1c368712e124e90ee912ec8615afa78974ddad19",
		},
	}).Source

	if s.Repository != "https://github.com/a/b.git" || s.RepositoryURL != "https://github.com/a/b" {
		t.Errorf("source = %+v", s)
	}
}

func TestBuilderLink(t *testing.T) {
	linked := summaryOf(
		&attestations.Provenance{BuilderID: "https://github.com/o/r/actions/runs/1"},
	)
	if linked.Builder == nil || linked.Builder.Href != "https://github.com/o/r/actions/runs/1" {
		t.Errorf("builder = %+v", linked.Builder)
	}

	plain := summaryOf(&attestations.Provenance{BuilderID: "docker-library"})
	if plain.Builder == nil || plain.Builder.Href != "" || plain.Builder.Value != "docker-library" {
		t.Errorf("builder = %+v", plain.Builder)
	}
}

func TestCILinks(t *testing.T) {
	ci := &attestations.GitHubContext{
		Actor:       "crazy-max",
		ServerURL:   "https://github.com",
		Repository:  "docker/buildkit-syft-scanner",
		RunID:       "40359464897",
		RunAttempt:  "1",
		WorkflowRef: "docker/buildkit-syft-scanner/.github/workflows/ci.yaml@refs/tags/v1.12.1",
		WorkflowSHA: "422dfad83311be8c7e03e219ab6e8f70f4619575",
	}

	got := summaryOf(&attestations.Provenance{CI: ci}).CI
	if got.RunURL != "https://github.com/docker/buildkit-syft-scanner/actions/runs/40359464897/attempts/1" {
		t.Errorf("run = %q", got.RunURL)
	}

	if got.WorkflowURL != "https://github.com/docker/buildkit-syft-scanner/blob/"+
		"422dfad83311be8c7e03e219ab6e8f70f4619575/.github/workflows/ci.yaml" ||
		got.Workflow != ".github/workflows/ci.yaml" {
		t.Errorf("workflow = %q at %q", got.Workflow, got.WorkflowURL)
	}

	plain := *ci
	plain.ServerURL = "http://github.example.com"

	if got := summaryOf(&attestations.Provenance{CI: &plain}).CI; got.RunURL != "" ||
		got.WorkflowURL != "" {
		t.Errorf("a plain-text server was linked: %+v", got)
	}
}

func TestServiceAttestationTargetOffersEveryPlatform(t *testing.T) {
	svc := swarm.Service{Spec: swarm.ServiceSpec{TaskTemplate: taskOn("", "").Spec}}

	got := ServiceAttestationTarget(context.Background(), svc,
		&fakeAttestations{image: imageWith(amd64, arm64)})
	if got.Image != svc.Spec.TaskTemplate.ContainerSpec.Image ||
		!slices.Equal(got.Candidates, []attestations.Platform{amd64, arm64}) {
		t.Errorf("got %+v", got)
	}

	if got := ServiceAttestationTarget(context.Background(), svc, nil); got.Attested.Status !=
		attestations.StatusDisabled {
		t.Errorf("nil source: %+v", got)
	}
}

func TestTaskAttestationTargetOffersItsNodesPlatforms(t *testing.T) {
	node := nodeOn("n1", "aarch64")

	got := TaskAttestationTarget(context.Background(), taskOn("n1", swarm.TaskStateRunning), &node,
		&fakeAttestations{image: imageWith(amd64, arm64)})
	if !slices.Equal(got.Candidates, []attestations.Platform{arm64}) {
		t.Errorf("candidates = %v", got.Candidates)
	}
}

func TestSBOMViewSummarises(t *testing.T) {
	view := NewSBOMView(amd64, &attestations.SBOM{
		Licensed:   1,
		Ecosystems: map[string]int{"deb": 2},
		Packages:   []attestations.Package{{Name: "a"}, {Name: "b"}},
	})

	if view.Platform != "linux/amd64" || view.Summary.Total != 2 || view.Summary.Licensed != 1 {
		t.Errorf("view = %+v", view)
	}

	if empty := NewSBOMView(amd64, &attestations.SBOM{}); empty.Packages == nil {
		t.Error("an empty SBOM lists null packages")
	}
}

func TestProvenanceDisabled(t *testing.T) {
	got := ServiceProvenance(context.Background(), swarm.Service{Spec: swarm.ServiceSpec{
		TaskTemplate: taskOn("", "").Spec,
	}}, nil, nil, nil)

	if got.Status != attestations.StatusDisabled || len(got.Platforms) != 0 {
		t.Errorf("got %+v", got)
	}
}
