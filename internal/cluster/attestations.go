package cluster

import (
	"context"
	"errors"
	"net/url"
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

var (
	ErrPlatformRequired = errors.New("several platforms; name one")
	ErrPlatformUnknown  = errors.New("platform not among the candidates")
)

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
	Platform       string             `json:"platform"`
	ManifestDigest string             `json:"manifestDigest"`
	RunningTasks   int                `json:"runningTasks"`
	Provenance     *ProvenanceSummary `json:"provenance,omitempty"`
	SBOM           *SBOMDescriptor    `json:"sbom,omitempty"`
	Dockerfile     bool               `json:"dockerfile"`
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

// ServiceProvenance projects the attestations of the image svc's spec names,
// counting its running tasks per platform.
func ServiceProvenance(
	ctx context.Context,
	svc swarm.Service,
	tasks []swarm.Task,
	nodes func(id string) (swarm.Node, bool),
	src AttestationSource,
) ImageProvenance {
	image := containerImage(svc.Spec.TaskTemplate.ContainerSpec)
	if src == nil {
		return ImageProvenance{Image: image, Status: attestations.StatusDisabled}
	}

	img := src.Image(ctx, image)
	out := project(image, img, img.Platforms)

	for i, p := range img.Platforms {
		for _, task := range tasks {
			if task.Status.State != swarm.TaskStateRunning || nodes == nil {
				continue
			}

			if node, ok := nodes(task.NodeID); ok && nodeRuns(node, p.Platform) {
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
	node *swarm.Node,
	src AttestationSource,
) ImageProvenance {
	image := containerImage(task.Spec.ContainerSpec)
	if src == nil {
		return ImageProvenance{Image: image, Status: attestations.StatusDisabled}
	}

	img := src.Image(ctx, image)

	var platforms []attestations.ImagePlatform

	for _, candidate := range TaskPlatforms(task, node, img) {
		for _, p := range img.Platforms {
			if p.Platform == candidate {
				platforms = append(platforms, p)
			}
		}
	}

	return project(image, img, platforms)
}

// TaskPlatforms are the platforms of img a task's node can be running. The
// node does not report an ARM variant, so every variant matches; a task not
// placed yet could run any.
func TaskPlatforms(
	task swarm.Task,
	node *swarm.Node,
	img attestations.Image,
) []attestations.Platform {
	var out []attestations.Platform

	for _, p := range img.Platforms {
		if task.NodeID == "" || node == nil || nodeRuns(*node, p.Platform) {
			out = append(out, p.Platform)
		}
	}

	return out
}

// SelectPlatform picks the requested platform among candidates, or the only
// one when none is requested.
func SelectPlatform(
	candidates []attestations.Platform,
	requested string,
) (attestations.Platform, error) {
	if requested == "" {
		if len(candidates) == 1 {
			return candidates[0], nil
		}

		return attestations.Platform{}, ErrPlatformRequired
	}

	for _, c := range candidates {
		if c.String() == requested {
			return c, nil
		}
	}

	return attestations.Platform{}, ErrPlatformUnknown
}

func nodeRuns(node swarm.Node, p attestations.Platform) bool {
	have := node.Description.Platform

	return strings.EqualFold(have.OS, p.OS) &&
		normalizeArch(have.Architecture) == normalizeArch(p.Architecture)
}

func project(
	image string,
	img attestations.Image,
	platforms []attestations.ImagePlatform,
) ImageProvenance {
	out := ImageProvenance{Image: image, Status: img.Status}
	if !img.RetryAfter.IsZero() {
		out.RetryAfter = new(img.RetryAfter)
	}

	for _, p := range platforms {
		pp := PlatformProvenance{
			Platform:       p.Platform.String(),
			ManifestDigest: p.ManifestDigest,
			Provenance:     summarise(p.Provenance),
			Dockerfile:     p.Provenance != nil && p.Provenance.HasSource,
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
		Inputs:         p.Inputs,
		CI:             ciSummary(p.CI),
	}

	if p.BuilderID != "" {
		s.Builder = &LinkedValue{Value: p.BuilderID, Href: httpsLink(p.BuilderID)}
	}

	return s
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
		s.WorkflowURL = base + "/blob/" + url.PathEscape(ci.WorkflowSHA) + "/" + workflow
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
