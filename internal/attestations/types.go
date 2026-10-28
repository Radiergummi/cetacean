// Package attestations reads the provenance and SBOMs BuildKit attaches to an
// image index, fetching them from the image's registry by digest. It knows
// OCI, SLSA and SPDX, and nothing about Swarm.
package attestations

import "time"

// Platform is one platform of an image index, in OCI's names.
type Platform struct {
	OS, Architecture, Variant string
}

func (p Platform) String() string {
	s := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		s += "/" + p.Variant
	}

	return s
}

// Status is what can be said about an image's attestations.
type Status string

const (
	StatusAvailable   Status = "available"
	StatusNone        Status = "none"
	StatusUnpinned    Status = "unpinned"
	StatusPrivate     Status = "private"
	StatusMissing     Status = "missing"
	StatusUnavailable Status = "unavailable"
	StatusDisabled    Status = "disabled"
)

// Descriptor names content by digest and size.
type Descriptor struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// Attested is one platform of an index and the attestations stored for it.
type Attested struct {
	Platform       Platform
	ManifestDigest string
	Provenance     *Descriptor
	SBOM           *Descriptor
}

// Provenance is what a SLSA predicate says about how an image was built. A
// field the predicate does not carry is left zero.
type Provenance struct {
	Source         *Source
	DockerfilePath string
	BuilderID      string
	StartedOn      *time.Time
	FinishedOn     *time.Time
	Frontend       string
	Reproducible   *bool
	Inputs         []Input
	CI             *GitHubContext

	// HasSource is true when the predicate embeds the Dockerfile, which only
	// mode=max provenance does.
	HasSource bool
}

// Source is the repository an image was built from.
type Source struct {
	Repository, Ref, Commit, Subdirectory string
}

// Input is one dependency of a build. Image is set for a container image,
// rendered from its package URL.
type Input struct {
	URI    string `json:"uri"`
	Digest string `json:"digest,omitempty"`
	Image  string `json:"image,omitempty"`
}

// GitHubContext is the job a build ran in on GitHub Actions.
type GitHubContext struct {
	Actor, TriggeringActor, Event, Ref, WorkflowRef, WorkflowSHA string
	ServerURL, Repository, RunID, RunAttempt, RunnerEnvironment  string
}

// SBOM is the package list of an SPDX document.
type SBOM struct {
	Generators []string
	Created    *time.Time
	Packages   []Package
	Ecosystems map[string]int
	Licensed   int
}

// Package is one entry of an SBOM.
type Package struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`
	License   string `json:"license"`
	PURL      string `json:"purl"`
	FoundIn   string `json:"foundIn"`
}

// Dockerfile is an embedded Dockerfile with each instruction's layers.
type Dockerfile struct {
	Path         string           `json:"path"`
	Lines        []string         `json:"lines"`
	Steps        []DockerfileStep `json:"steps"`
	Unattributed []Descriptor     `json:"unattributed"`
	TotalSize    int64            `json:"totalSize"`
	BaseSize     int64            `json:"baseSize"`
}

// DockerfileStep is one build step: the lines it was written on, 1-based and
// inclusive, and the layers it produced. Base marks a FROM.
type DockerfileStep struct {
	Ranges [][2]int     `json:"ranges"`
	Base   bool         `json:"base"`
	Layers []Descriptor `json:"layers"`
}
