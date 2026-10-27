package attestations

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

// ErrNoSource is provenance that does not embed the Dockerfile it was built
// from, which only mode=max provenance does.
var ErrNoSource = errors.New("provenance embeds no Dockerfile")

var commitGrammar = regexp.MustCompile(`^[0-9a-f]{40}$`)

type statement struct {
	PredicateType string          `json:"predicateType"`
	Predicate     json.RawMessage `json:"predicate"`
}

type configSource struct {
	URI        string            `json:"uri"`
	Digest     map[string]string `json:"digest"`
	EntryPoint string            `json:"entryPoint"` // v0.2
	Path       string            `json:"path"`       // v1
}

type material struct {
	URI    string            `json:"uri"`
	Digest map[string]string `json:"digest"`
}

// buildkitMetadata is BuildKit's extension, under a URI key in v0.2 and under
// buildkit_metadata in v1.
type buildkitMetadata struct {
	VCS    map[string]string         `json:"vcs"`
	Source *buildkitSource           `json:"source"`
	Layers map[string][][]Descriptor `json:"layers"`
}

type buildkitSource struct {
	Infos []struct {
		Filename string `json:"filename"`
		Language string `json:"language"`
		Data     []byte `json:"data"`
	} `json:"infos"`
	Locations map[string]struct {
		Locations []struct {
			SourceIndex int `json:"sourceIndex"`
			Ranges      []struct {
				Start struct {
					Line int `json:"line"`
				} `json:"start"`
				End struct {
					Line int `json:"line"`
				} `json:"end"`
			} `json:"ranges"`
		} `json:"locations"`
	} `json:"locations"`
}

// dockerfileIndex is the index of the embedded Dockerfile among the sources,
// or -1.
func (s *buildkitSource) dockerfileIndex() int {
	if s == nil {
		return -1
	}

	for i, info := range s.Infos {
		if info.Language == "Dockerfile" {
			return i
		}
	}

	return -1
}

type buildConfig struct {
	LLBDefinition []struct {
		ID string `json:"id"`
		Op struct {
			Op map[string]json.RawMessage `json:"Op"`
		} `json:"op"`
	} `json:"llbDefinition"`
}

type predicateV02 struct {
	Builder struct {
		ID string `json:"id"`
	} `json:"builder"`
	Invocation struct {
		ConfigSource configSource `json:"configSource"`
		Parameters   struct {
			Frontend string `json:"frontend"`
		} `json:"parameters"`
	} `json:"invocation"`
	Materials []material `json:"materials"`
	Metadata  struct {
		BuildStartedOn  string            `json:"buildStartedOn"`
		BuildFinishedOn string            `json:"buildFinishedOn"`
		Reproducible    *bool             `json:"reproducible"`
		BuildKit        *buildkitMetadata `json:"https://mobyproject.org/buildkit@v1#metadata"`
	} `json:"metadata"`
	BuildConfig *buildConfig `json:"buildConfig"`
}

type predicateV1 struct {
	BuildDefinition struct {
		ExternalParameters struct {
			ConfigSource configSource `json:"configSource"`
			Request      struct {
				Frontend string `json:"frontend"`
			} `json:"request"`
		} `json:"externalParameters"`
		InternalParameters   map[string]json.RawMessage `json:"internalParameters"`
		ResolvedDependencies []material                 `json:"resolvedDependencies"`
	} `json:"buildDefinition"`
	RunDetails struct {
		Builder struct {
			ID string `json:"id"`
		} `json:"builder"`
		Metadata struct {
			StartedOn  string            `json:"startedOn"`
			FinishedOn string            `json:"finishedOn"`
			BuildKit   *buildkitMetadata `json:"buildkit_metadata"`
		} `json:"metadata"`
	} `json:"runDetails"`
}

// parsed is a predicate of either version, reduced to what is read from it.
type parsed struct {
	provenance  Provenance
	buildkit    *buildkitMetadata
	buildConfig *buildConfig
}

func parseStatement(body []byte) (parsed, error) {
	var st statement
	if err := json.Unmarshal(body, &st); err != nil {
		return parsed{}, fmt.Errorf("statement: %w", err)
	}

	// An omitted predicate reads as an empty one.
	if len(st.Predicate) == 0 || string(st.Predicate) == "null" {
		st.Predicate = json.RawMessage("{}")
	}

	switch st.PredicateType {
	case predicateSLSAv02:
		var p predicateV02
		if err := json.Unmarshal(st.Predicate, &p); err != nil {
			return parsed{}, fmt.Errorf("provenance v0.2: %w", err)
		}

		return fromV02(p), nil
	case predicateSLSAv1:
		var p predicateV1
		if err := json.Unmarshal(st.Predicate, &p); err != nil {
			return parsed{}, fmt.Errorf("provenance v1: %w", err)
		}

		return fromV1(p), nil
	default:
		return parsed{}, fmt.Errorf("predicate %q is not SLSA provenance", st.PredicateType)
	}
}

// ParseProvenance reads a SLSA v0.2 or v1 provenance statement.
func ParseProvenance(body []byte) (*Provenance, error) {
	p, err := parseStatement(body)
	if err != nil {
		return nil, err
	}

	return &p.provenance, nil
}

func fromV02(p predicateV02) parsed {
	cs := p.Invocation.ConfigSource
	bk := p.Metadata.BuildKit

	return parsed{
		provenance: Provenance{
			Source:         sourceOf(cs, bk),
			DockerfilePath: cs.EntryPoint,
			BuilderID:      p.Builder.ID,
			StartedOn:      parseTime(p.Metadata.BuildStartedOn),
			FinishedOn:     parseTime(p.Metadata.BuildFinishedOn),
			Frontend:       p.Invocation.Parameters.Frontend,
			Reproducible:   p.Metadata.Reproducible,
			Inputs:         inputsOf(p.Materials),
			HasSource:      bk != nil && bk.Source.dockerfileIndex() >= 0,
		},
		buildkit:    bk,
		buildConfig: p.BuildConfig,
	}
}

func fromV1(p predicateV1) parsed {
	cs := p.BuildDefinition.ExternalParameters.ConfigSource
	bk := p.RunDetails.Metadata.BuildKit
	internal := p.BuildDefinition.InternalParameters

	var config *buildConfig
	if raw, ok := internal["buildConfig"]; ok {
		config = new(buildConfig)
		if json.Unmarshal(raw, config) != nil {
			config = nil
		}
	}

	return parsed{
		provenance: Provenance{
			Source:         sourceOf(cs, bk),
			DockerfilePath: cs.Path,
			BuilderID:      p.RunDetails.Builder.ID,
			StartedOn:      parseTime(p.RunDetails.Metadata.StartedOn),
			FinishedOn:     parseTime(p.RunDetails.Metadata.FinishedOn),
			Frontend:       p.BuildDefinition.ExternalParameters.Request.Frontend,
			Inputs:         inputsOf(p.BuildDefinition.ResolvedDependencies),
			CI:             githubContext(internal),
			HasSource:      bk != nil && bk.Source.dockerfileIndex() >= 0,
		},
		buildkit:    bk,
		buildConfig: config,
	}
}

// sourceOf reads a git context from the config source, `<repo>#<ref>[:<dir>]`,
// and falls back to the vcs metadata buildx records for a local checkout.
func sourceOf(cs configSource, bk *buildkitMetadata) *Source {
	if cs.URI != "" {
		repository, fragment, _ := strings.Cut(cs.URI, "#")
		ref, subdirectory, _ := strings.Cut(fragment, ":")

		commit := cs.Digest["sha1"]
		if commit == "" && commitGrammar.MatchString(ref) {
			commit = ref
		}

		return &Source{Repository: repository, Ref: ref, Commit: commit, Subdirectory: subdirectory}
	}

	if bk != nil && bk.VCS["source"] != "" {
		return &Source{Repository: bk.VCS["source"], Commit: bk.VCS["revision"]}
	}

	return nil
}

func inputsOf(materials []material) []Input {
	var out []Input

	for _, m := range materials {
		if m.URI == "" {
			continue
		}

		in := Input{URI: m.URI, Digest: digestOf(m.Digest), Image: imageOfPURL(m.URI)}
		out = append(out, in)
	}

	return out
}

// digestOf renders an in-toto digest set as one algorithm:hex, preferring sha256.
func digestOf(set map[string]string) string {
	if v, ok := set["sha256"]; ok {
		return "sha256:" + v
	}

	algorithms := make([]string, 0, len(set))
	for a := range set {
		algorithms = append(algorithms, a)
	}

	if len(algorithms) == 0 {
		return ""
	}

	sort.Strings(algorithms)

	return algorithms[0] + ":" + set[algorithms[0]]
}

// imageOfPURL renders pkg:docker/<name>@<version>?digest=… as an image
// reference, or "" for anything that is not a docker package URL.
func imageOfPURL(purl string) string {
	rest, ok := strings.CutPrefix(purl, "pkg:docker/")
	if !ok {
		return ""
	}

	rest, query, _ := strings.Cut(rest, "?")
	name, version, _ := strings.Cut(rest, "@")

	image := name
	if version != "" {
		image += ":" + version
	}

	if values, err := url.ParseQuery(query); err == nil && values.Get("digest") != "" {
		image += "@" + values.Get("digest")
	}

	return image
}

func githubContext(internal map[string]json.RawMessage) *GitHubContext {
	str := func(key string) string {
		var s string
		if raw, ok := internal[key]; ok {
			_ = json.Unmarshal(raw, &s)
		}

		return s
	}

	if str("github_run_id") == "" {
		return nil
	}

	return &GitHubContext{
		Actor:             str("github_actor"),
		TriggeringActor:   str("github_triggering_actor"),
		Event:             str("github_event_name"),
		Ref:               str("github_ref"),
		WorkflowRef:       str("github_workflow_ref"),
		WorkflowSHA:       str("github_workflow_sha"),
		ServerURL:         str("github_server_url"),
		Repository:        str("github_repository"),
		RunID:             str("github_run_id"),
		RunAttempt:        str("github_run_attempt"),
		RunnerEnvironment: str("github_runner_environment"),
	}
}

// parseTime reads an RFC 3339 timestamp; SLSA treats unset, null and empty
// alike, so anything unreadable is absent.
func parseTime(s string) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}

	return &t
}
