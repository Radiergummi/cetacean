package integrations

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// DocoCDIntegration represents the deployment provenance doco-cd writes onto
// the services it deploys.
type DocoCDIntegration struct {
	Name       string     `json:"name"`
	Enabled    bool       `json:"enabled"`
	Labels     []string   `json:"labels"`
	Deployment string     `json:"deployment,omitempty"`
	Source     string     `json:"source,omitempty"`
	Repository string     `json:"repository,omitempty"`
	SourceURL  string     `json:"sourceUrl,omitempty"`
	Ref        string     `json:"ref,omitempty"`
	Commit     string     `json:"commit,omitempty"`
	CommitURL  string     `json:"commitUrl,omitempty"`
	Trigger    string     `json:"trigger,omitempty"`
	DeployedAt *time.Time `json:"deployedAt,omitempty"`
	Version    string     `json:"version,omitempty"`
	CertExpiry *time.Time `json:"certExpiry,omitempty"`
	Job        *DocoCDJob `json:"job,omitempty"`
}

// DocoCDJob represents a doco-cd scheduled job and its runtime state.
type DocoCDJob struct {
	Enabled       bool       `json:"enabled"`
	Schedule      string     `json:"schedule,omitempty"`
	ExecutionMode string     `json:"executionMode,omitempty"`
	SkipRunning   *bool      `json:"skipRunning,omitempty"`
	LastRun       *time.Time `json:"lastRun,omitempty"`
	NextRun       *time.Time `json:"nextRun,omitempty"`
}

func detectDocoCD(labels map[string]string) *DocoCDIntegration {
	integration := &DocoCDIntegration{Name: "doco-cd", Enabled: true}

	for k, v := range labels {
		suffix, ok := strings.CutPrefix(k, "cd.doco.")
		if !ok {
			continue
		}

		integration.Labels = append(integration.Labels, k)

		if jobKey, ok := strings.CutPrefix(suffix, "job."); ok {
			if integration.Job == nil {
				integration.Job = &DocoCDJob{}
			}

			readDocoJobLabel(integration.Job, jobKey, v)

			continue
		}

		switch suffix {
		case "metadata.version":
			integration.Version = v
		case "deployment.name":
			integration.Deployment = v
		case "deployment.timestamp":
			integration.DeployedAt = parseDocoTime(v)
		case "deployment.trigger":
			integration.Trigger = v
		case "deployment.target.ref":
			integration.Ref = v
		case "deployment.target.sha":
			integration.Commit = v
		case "deployment.cert.expiry":
			integration.CertExpiry = parseDocoTime(v)
		case "source":
			integration.Source = v
		case "source.name":
			integration.Repository = v
		case "source.url":
			integration.SourceURL = stripURLCredentials(v)
		}
	}

	if len(integration.Labels) == 0 {
		return nil
	}

	slices.Sort(integration.Labels)

	if integration.Source != "oci" {
		integration.CommitURL = docoCommitURL(integration.SourceURL, integration.Commit)
	}

	return integration
}

func readDocoJobLabel(job *DocoCDJob, key, value string) {
	switch key {
	case "enabled":
		job.Enabled = value == "true"
	case "schedule":
		job.Schedule = value
	case "execution_mode":
		job.ExecutionMode = value
	case "skip_running":
		job.SkipRunning = new(value == "true")
	case "last_run":
		job.LastRun = parseDocoTime(value)
	case "next_run":
		job.NextRun = parseDocoTime(value)
	}
}

func parseDocoTime(value string) *time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}

	return &t
}

// stripURLCredentials drops the userinfo of a URL, which for a clone URL can
// carry an access token. A URL that doesn't parse, such as one whose password
// holds a `/`, is dropped whole rather than risk passing a token through.
func stripURLCredentials(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	if u.User == nil || u.Host == "" {
		return raw
	}

	u.User = nil

	return u.String()
}

var (
	commitSHA = regexp.MustCompile(`^(?i)[0-9a-f]{7,64}$`)
	scpLike   = regexp.MustCompile(`^[^@/]+@([^:/]+):(.+)$`)
)

// docoCommitURL links a commit on a forge recognised by its host name, or
// returns "" when the forge, and so its commit URL shape, is unknown.
func docoCommitURL(source, sha string) string {
	if !commitSHA.MatchString(sha) {
		return ""
	}

	if m := scpLike.FindStringSubmatch(source); m != nil {
		source = "ssh://" + m[1] + "/" + m[2]
	}

	u, err := url.Parse(source)
	if err != nil || u.Host == "" {
		return ""
	}

	web := url.URL{Scheme: "https", Host: u.Hostname()}

	switch u.Scheme {
	case "http", "https":
		web.Scheme, web.Host = u.Scheme, u.Host
	case "ssh", "git":
	default:
		return ""
	}

	repository := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if repository == "" {
		return ""
	}

	commitPath := forgeCommitPath(strings.ToLower(u.Hostname()))
	if commitPath == "" {
		return ""
	}

	web.Path = "/" + repository + commitPath + sha

	return web.String()
}

// forgeCommitPath names a forge by its public host, or by a first DNS label
// that is exactly the forge's name, as in gitlab.example.com.
func forgeCommitPath(host string) string {
	switch host {
	case "gitlab.com":
		return "/-/commit/"
	case "github.com", "gitea.com", "codeberg.org":
		return "/commit/"
	}

	first, _, _ := strings.Cut(host, ".")

	switch first {
	case "gitlab":
		return "/-/commit/"
	case "github", "gitea", "forgejo":
		return "/commit/"
	}

	return ""
}
