package integrations

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// DeploymentActionIntegration represents the labels
// matchory/docker-swarm-deployment-action writes onto the services it deploys,
// and the per-service overrides of its settings. Nil overrides are unset.
type DeploymentActionIntegration struct {
	Name                  string   `json:"name"`
	Enabled               bool     `json:"enabled"`
	Labels                []string `json:"labels"`
	Version               string   `json:"version,omitempty"`
	Repository            string   `json:"repository,omitempty"`
	RepositoryURL         string   `json:"repositoryUrl,omitempty"`
	Ref                   string   `json:"ref,omitempty"`
	Commit                string   `json:"commit,omitempty"`
	CommitURL             string   `json:"commitUrl,omitempty"`
	RunURL                string   `json:"runUrl,omitempty"`
	Actor                 string   `json:"actor,omitempty"`
	Monitor               *bool    `json:"monitor,omitempty"`
	MonitorTimeoutSeconds *int     `json:"monitorTimeoutSeconds,omitempty"`
	HealthCheckWarnings   *bool    `json:"healthCheckWarnings,omitempty"`
}

// actionRunURL is the run URL the action builds from GITHUB_SERVER_URL,
// GITHUB_REPOSITORY and GITHUB_RUN_ID.
var actionRunURL = regexp.MustCompile(`^(https?://[^/@\s]+)/([^/\s]+/[^/\s]+)/actions/runs/\d+$`)

func detectDeploymentAction(labels map[string]string) *DeploymentActionIntegration {
	integration := &DeploymentActionIntegration{
		Name:    "docker-swarm-deployment-action",
		Enabled: true,
	}

	for k, v := range labels {
		suffix, ok := strings.CutPrefix(k, "com.matchory.deployment.")
		if !ok {
			continue
		}

		integration.Labels = append(integration.Labels, k)

		switch suffix {
		case "version":
			integration.Version = v
		case "ref":
			integration.Ref = v
		case "commit":
			integration.Commit = v
		case "run-url":
			if m := actionRunURL.FindStringSubmatch(v); m != nil {
				integration.RunURL = v
				integration.Repository = m[2]
			}
		case "actor":
			integration.Actor = v
		case "monitor":
			integration.Monitor = parseActionBool(v)
		case "monitor-timeout":
			if n, err := strconv.ParseUint(v, 10, 31); err == nil && n > 0 {
				integration.MonitorTimeoutSeconds = new(int(n))
			}
		case "health-check-warnings":
			integration.HealthCheckWarnings = parseActionBool(v)
		}
	}

	if len(integration.Labels) == 0 {
		return nil
	}

	slices.Sort(integration.Labels)

	if integration.Repository != "" {
		server := actionRunURL.FindStringSubmatch(integration.RunURL)[1]
		integration.RepositoryURL = server + "/" + integration.Repository

		if commitSHA.MatchString(integration.Commit) {
			integration.CommitURL = integration.RepositoryURL + "/commit/" + integration.Commit
		}
	}

	return integration
}

// parseActionBool accepts the spellings the action does, or returns nil.
func parseActionBool(v string) *bool {
	switch v {
	case "true", "True", "TRUE":
		return new(true)
	case "false", "False", "FALSE":
		return new(false)
	}

	return nil
}
