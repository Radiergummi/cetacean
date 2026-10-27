package integrations

import (
	"slices"
	"strconv"
	"strings"
)

// CronjobIntegration represents parsed Swarm Cronjob configuration.
type CronjobIntegration struct {
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	Labels        []string `json:"labels"`
	Schedule      string   `json:"schedule,omitempty"`
	SkipRunning   bool     `json:"skipRunning,omitempty"`
	Replicas      int      `json:"replicas,omitempty"`
	RegistryAuth  bool     `json:"registryAuth,omitempty"`
	QueryRegistry bool     `json:"queryRegistry,omitempty"`
}

func detectCronjob(labels map[string]string) *CronjobIntegration {
	integration := &CronjobIntegration{Name: "swarm-cronjob"}

	for k, v := range labels {
		suffix, ok := strings.CutPrefix(k, "swarm.cronjob.")
		if !ok {
			continue
		}

		integration.Labels = append(integration.Labels, k)

		switch suffix {
		case "schedule":
			integration.Schedule = v
		case "skip-running":
			integration.SkipRunning = v == "true"
		case "replicas":
			if n, err := strconv.Atoi(v); err == nil {
				integration.Replicas = n
			}
		case "registry-auth":
			integration.RegistryAuth = v == "true"
		case "query-registry":
			integration.QueryRegistry = v == "true"
		}
	}

	if len(integration.Labels) == 0 {
		return nil
	}

	slices.Sort(integration.Labels)

	integration.Enabled = enabled(labels, "swarm.cronjob.")

	return integration
}
