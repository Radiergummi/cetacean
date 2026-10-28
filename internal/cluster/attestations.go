package cluster

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/attestations"
)

// AttestationSource answers what an image's registry holds about how it was
// built; *attestations.Store is one, and a nil one is the feature switched off.
type AttestationSource interface {
	Image(ctx context.Context, image string) attestations.Image
	SBOM(
		ctx context.Context,
		image string,
		p attestations.Platform,
	) (*attestations.SBOM, []byte, error)
	Dockerfile(
		ctx context.Context,
		image string,
		p attestations.Platform,
	) (*attestations.Dockerfile, error)
}

// ErrPlatformRequired is a request naming no platform of an image with
// several; one naming an unknown platform is attestations.ErrUnknownPlatform.
var ErrPlatformRequired = errors.New("several platforms; name one")

// ImageProvenance is what a service or task shows about its image's build.
type ImageProvenance struct {
	Image      string               `json:"image"`
	Status     attestations.Status  `json:"status"`
	RetryAfter *time.Time           `json:"retryAfter,omitempty"`
	Platforms  []PlatformProvenance `json:"platforms,omitempty"`
}

// PlatformProvenance is one platform of the image. RunningTasks counts a
// service's running tasks on nodes of this platform, and is 0 for a task.
type PlatformProvenance struct {
	Platform         string              `json:"platform"`
	ManifestDigest   string              `json:"manifestDigest"`
	RunningTasks     int                 `json:"runningTasks"`
	Provenance       *ProvenanceSummary  `json:"provenance,omitempty"`
	ProvenanceStatus attestations.Status `json:"provenanceStatus,omitempty"`
	SBOM             *SBOMDescriptor     `json:"sbom,omitempty"`
	Dockerfile       bool                `json:"dockerfile"`
}

type SBOMDescriptor struct {
	Digest   string `json:"digest"`
	Size     int64  `json:"size"`
	TooLarge bool   `json:"tooLarge"`
}

type ProvenanceSummary struct {
	Source         *SourceSummary       `json:"source,omitempty"`
	DockerfilePath string               `json:"dockerfilePath,omitempty"`
	Builder        *LinkedValue         `json:"builder,omitempty"`
	StartedOn      *time.Time           `json:"startedOn,omitempty"`
	FinishedOn     *time.Time           `json:"finishedOn,omitempty"`
	Frontend       string               `json:"frontend,omitempty"`
	Reproducible   *bool                `json:"reproducible,omitempty"`
	Inputs         []attestations.Input `json:"inputs,omitempty"`
	CI             *CISummary           `json:"ci,omitempty"`
}

type SourceSummary struct {
	Repository    string `json:"repository"`
	Ref           string `json:"ref,omitempty"`
	Commit        string `json:"commit,omitempty"`
	Subdirectory  string `json:"subdirectory,omitempty"`
	RepositoryURL string `json:"repositoryUrl,omitempty"`
	CommitURL     string `json:"commitUrl,omitempty"`
}

// LinkedValue is a value with a link to it, when it is a safe https URL.
type LinkedValue struct {
	Value string `json:"value"`
	Href  string `json:"href,omitempty"`
}

// CISummary is the GitHub Actions job an image was built in.
type CISummary struct {
	Actor             string `json:"actor,omitempty"`
	TriggeringActor   string `json:"triggeringActor,omitempty"`
	Event             string `json:"event,omitempty"`
	Ref               string `json:"ref,omitempty"`
	Workflow          string `json:"workflow,omitempty"`
	WorkflowSHA       string `json:"workflowSha,omitempty"`
	RunID             string `json:"runId,omitempty"`
	RunAttempt        string `json:"runAttempt,omitempty"`
	RunnerEnvironment string `json:"runnerEnvironment,omitempty"`
	RunURL            string `json:"runUrl,omitempty"`
	WorkflowURL       string `json:"workflowUrl,omitempty"`
}

// Hosts whose commit pages Cetacean knows how to address.
var commitPaths = map[string]string{
	"github.com":   "/commit/",
	"gitlab.com":   "/-/commit/",
	"codeberg.org": "/commit/",
}

func containerImage(spec *swarm.ContainerSpec) string {
	if spec == nil {
		return ""
	}

	return spec.Image
}

// NodeLookup finds a node by ID; the cache's GetNode is one.
type NodeLookup func(id string) (swarm.Node, bool)

// ServiceProvenance projects the attestations of the image svc's spec names,
// counting its running tasks per platform.
func ServiceProvenance(
	ctx context.Context,
	svc swarm.Service,
	tasks []swarm.Task,
	nodes NodeLookup,
	src AttestationSource,
) ImageProvenance {
	t := ServiceAttestationTarget(ctx, svc, src)
	out := project(t)

	for _, task := range tasks {
		if task.Status.State != swarm.TaskStateRunning || nodes == nil {
			continue
		}

		node, ok := nodes(task.NodeID)
		if !ok {
			continue
		}

		runs := nodePlatforms(node, t.Attested.Platforms)

		for i, p := range t.Attested.Platforms {
			if slices.Contains(runs, p.Platform) {
				out.Platforms[i].RunningTasks++
			}
		}
	}

	return out
}

// TaskProvenance projects the attestations of the image task runs, for the
// platforms of the node it is on.
func TaskProvenance(
	ctx context.Context,
	task swarm.Task,
	nodes NodeLookup,
	src AttestationSource,
) ImageProvenance {
	return project(TaskAttestationTarget(ctx, task, nodes, src))
}

// taskPlatforms are the platforms of img a task's node can be running; a
// task not placed yet could run any.
func taskPlatforms(
	task swarm.Task,
	node *swarm.Node,
	img attestations.Image,
) []attestations.Platform {
	if task.NodeID == "" || node == nil {
		var out []attestations.Platform
		for _, p := range img.Platforms {
			out = append(out, p.Platform)
		}

		return out
	}

	return nodePlatforms(*node, img.Platforms)
}

// selectPlatform picks the requested platform among candidates, or the only
// one when none is requested. Its errors name the candidates.
func selectPlatform(
	candidates []attestations.Platform,
	requested string,
) (attestations.Platform, error) {
	if requested == "" {
		if len(candidates) == 1 {
			return candidates[0], nil
		}

		return attestations.Platform{}, fmt.Errorf("%w: this image has %s",
			ErrPlatformRequired, platformNames(candidates))
	}

	for _, c := range candidates {
		if c.String() == requested {
			return c, nil
		}
	}

	return attestations.Platform{}, fmt.Errorf("%w: %q; this image has %s",
		attestations.ErrUnknownPlatform, requested, platformNames(candidates))
}

// platformNames lists platforms for a message.
func platformNames(platforms []attestations.Platform) string {
	if len(platforms) == 0 {
		return "no platform"
	}

	names := make([]string, len(platforms))
	for i, p := range platforms {
		names[i] = p.String()
	}

	return strings.Join(names, ", ")
}

// AttestationTarget is the image a service or task runs, what its registry
// holds about it, and the platforms a request for an SBOM or a Dockerfile may
// name.
type AttestationTarget struct {
	Image      string
	Attested   attestations.Image
	Candidates []attestations.Platform
}

// Platform answers a request naming requested: the image's own status as an
// error when its attestations cannot be read, otherwise a candidate.
func (t AttestationTarget) Platform(requested string) (attestations.Platform, error) {
	if err := t.Attested.Err(); err != nil {
		return attestations.Platform{}, err
	}

	return selectPlatform(t.Candidates, requested)
}

// ServiceAttestationTarget offers every platform of the image svc's spec names.
func ServiceAttestationTarget(
	ctx context.Context,
	svc swarm.Service,
	src AttestationSource,
) AttestationTarget {
	t := AttestationTarget{Image: containerImage(svc.Spec.TaskTemplate.ContainerSpec)}
	t.Attested = imageOf(ctx, src, t.Image)

	for _, p := range t.Attested.Platforms {
		t.Candidates = append(t.Candidates, p.Platform)
	}

	return t
}

// TaskAttestationTarget offers the platforms of the image task runs that its
// node can run.
func TaskAttestationTarget(
	ctx context.Context,
	task swarm.Task,
	nodes NodeLookup,
	src AttestationSource,
) AttestationTarget {
	var node *swarm.Node
	if nodes != nil {
		if n, ok := nodes(task.NodeID); ok {
			node = &n
		}
	}

	t := AttestationTarget{Image: containerImage(task.Spec.ContainerSpec)}
	t.Attested = imageOf(ctx, src, t.Image)
	t.Candidates = taskPlatforms(task, node, t.Attested)

	return t
}

func imageOf(ctx context.Context, src AttestationSource, image string) attestations.Image {
	if src == nil {
		return attestations.Image{Status: attestations.StatusDisabled}
	}

	return src.Image(ctx, image)
}

// SBOMView is one platform's SBOM as both transports render it.
type SBOMView struct {
	Platform   string                 `json:"platform"`
	Generators []string               `json:"generators"`
	Created    *time.Time             `json:"created,omitempty"`
	Summary    SBOMSummary            `json:"summary"`
	Packages   []attestations.Package `json:"packages"`
}

type SBOMSummary struct {
	Total      int            `json:"total"`
	Licensed   int            `json:"licensed"`
	Ecosystems map[string]int `json:"ecosystems"`
}

func NewSBOMView(p attestations.Platform, sbom *attestations.SBOM) SBOMView {
	packages := sbom.Packages
	if packages == nil {
		packages = []attestations.Package{}
	}

	return SBOMView{
		Platform:   p.String(),
		Generators: sbom.Generators,
		Created:    sbom.Created,
		Summary: SBOMSummary{
			Total: len(packages), Licensed: sbom.Licensed, Ecosystems: sbom.Ecosystems,
		},
		Packages: packages,
	}
}

// DockerfileView is one platform's Dockerfile with its layers.
type DockerfileView struct {
	Platform string `json:"platform"`
	*attestations.Dockerfile
}

// nodePlatforms are the platforms of an image a node pulls. A 32-bit ARM node
// names its version (armv7l), and gets the highest variant not above it, as
// containerd picks; one naming none (arm) may run any variant.
func nodePlatforms(
	node swarm.Node,
	platforms []attestations.ImagePlatform,
) []attestations.Platform {
	have := node.Description.Platform
	version := armVersion(have.Architecture)
	arch := normalizeArch(have.Architecture)

	if version > 0 {
		arch = "arm"
	}

	var out []attestations.Platform

	best := 0

	for _, ip := range platforms {
		p := ip.Platform
		if !strings.EqualFold(have.OS, p.OS) || normalizeArch(p.Architecture) != arch {
			continue
		}

		if arch == "arm" && version > 0 {
			v := armVersion("arm" + p.Variant)
			if v == 0 && p.Variant == "" {
				v = 7 // containerd reads linux/arm without a variant as v7
			}

			if v == 0 || v > version || v < best {
				continue
			}

			if v > best {
				best, out = v, out[:0]
			}
		}

		out = append(out, p)
	}

	return out
}

// armVersion is the ARM version an architecture or variant names: 7 for
// armv7l and for arm with variant v7, 0 when it names none from 5 to 7.
func armVersion(arch string) int {
	rest, ok := strings.CutPrefix(arch, "armv")
	if !ok || rest == "" || rest[0] < '5' || rest[0] > '7' {
		return 0
	}

	return int(rest[0] - '0')
}

// project renders the candidate platforms of t's image.
func project(t AttestationTarget) ImageProvenance {
	out := ImageProvenance{Image: t.Image, Status: t.Attested.Status}
	if !t.Attested.RetryAfter.IsZero() {
		out.RetryAfter = new(t.Attested.RetryAfter)
	}

	for _, p := range t.Attested.Platforms {
		if !slices.Contains(t.Candidates, p.Platform) {
			continue
		}

		pp := PlatformProvenance{
			Platform:         p.Platform.String(),
			ManifestDigest:   p.ManifestDigest,
			Provenance:       summarise(p.Provenance),
			ProvenanceStatus: p.ProvenanceStatus,
			Dockerfile:       p.Provenance != nil && p.Provenance.HasSource,
		}

		if p.SBOM != nil {
			pp.SBOM = &SBOMDescriptor{
				Digest:   p.SBOM.Digest,
				Size:     p.SBOM.Size,
				TooLarge: p.SBOMTooLarge,
			}
		}

		out.Platforms = append(out.Platforms, pp)
	}

	return out
}

func summarise(p *attestations.Provenance) *ProvenanceSummary {
	if p == nil {
		return nil
	}

	s := &ProvenanceSummary{
		Source:         sourceSummary(p.Source),
		DockerfilePath: p.DockerfilePath,
		StartedOn:      p.StartedOn,
		FinishedOn:     p.FinishedOn,
		Frontend:       p.Frontend,
		Reproducible:   p.Reproducible,
		Inputs:         strippedInputs(p.Inputs),
		CI:             ciSummary(p.CI),
	}

	if p.BuilderID != "" {
		s.Builder = &LinkedValue{Value: p.BuilderID, Href: httpsLink(p.BuilderID)}
	}

	return s
}

// strippedInputs drops credentials an input URI may carry, as for the source.
func strippedInputs(inputs []attestations.Input) []attestations.Input {
	out := make([]attestations.Input, len(inputs))
	for i, in := range inputs {
		in.URI = stripUserinfo(in.URI)
		out[i] = in
	}

	return out
}

func sourceSummary(src *attestations.Source) *SourceSummary {
	if src == nil {
		return nil
	}

	s := &SourceSummary{
		Repository:   stripUserinfo(src.Repository),
		Ref:          src.Ref,
		Commit:       src.Commit,
		Subdirectory: src.Subdirectory,
	}

	repository := httpsLink(strings.TrimSuffix(s.Repository, ".git"))
	if repository == "" {
		return s
	}

	u, err := url.Parse(repository)
	if err != nil {
		return s
	}

	commitPath, known := commitPaths[strings.ToLower(u.Host)]
	if !known {
		return s
	}

	s.RepositoryURL = repository
	if s.Commit != "" {
		s.CommitURL = repository + commitPath + url.PathEscape(s.Commit)
	}

	return s
}

func ciSummary(ci *attestations.GitHubContext) *CISummary {
	if ci == nil {
		return nil
	}

	workflow, _, _ := strings.Cut(strings.TrimPrefix(ci.WorkflowRef, ci.Repository+"/"), "@")

	s := &CISummary{
		Actor:             ci.Actor,
		Event:             ci.Event,
		Ref:               ci.Ref,
		Workflow:          workflow,
		WorkflowSHA:       ci.WorkflowSHA,
		RunID:             ci.RunID,
		RunAttempt:        ci.RunAttempt,
		RunnerEnvironment: ci.RunnerEnvironment,
	}

	if ci.TriggeringActor != ci.Actor {
		s.TriggeringActor = ci.TriggeringActor
	}

	base := httpsLink(strings.TrimSuffix(ci.ServerURL, "/") + "/" + ci.Repository)
	if base == "" || ci.Repository == "" {
		return s
	}

	if ci.RunID != "" {
		s.RunURL = base + "/actions/runs/" + url.PathEscape(ci.RunID)
		if ci.RunAttempt != "" {
			s.RunURL += "/attempts/" + url.PathEscape(ci.RunAttempt)
		}
	}

	if workflow != "" && ci.WorkflowSHA != "" {
		segments := strings.Split(workflow, "/")
		for i, segment := range segments {
			segments[i] = url.PathEscape(segment)
		}

		s.WorkflowURL = base + "/blob/" + url.PathEscape(ci.WorkflowSHA) + "/" +
			strings.Join(segments, "/")
	}

	return s
}

// httpsLink is value as a link when it is a safe https URL, and "" otherwise.
func httpsLink(value string) string {
	if !strings.HasPrefix(strings.ToLower(value), "https://") {
		return ""
	}

	link, ok := normaliseHTTPURL(value)
	if !ok {
		return ""
	}

	return link
}
