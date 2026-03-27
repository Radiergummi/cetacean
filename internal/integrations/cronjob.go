package integrations

import (
	"strconv"
	"strings"
)

// CronjobIntegration represents parsed Swarm Cronjob configuration.
type CronjobIntegration struct {
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Schedule    string `json:"schedule,omitempty"`
	SkipRunning bool   `json:"skipRunning,omitempty"`
	Replicas    int    `json:"replicas,omitempty"`
}

func detectCronjob(labels map[string]string) *CronjobIntegration {
	var found bool

	for k := range labels {
		if strings.HasPrefix(k, "swarm.cronjob.") {
			found = true
			break
		}
	}

	if !found {
		return nil
	}

	integration := &CronjobIntegration{
		Name:    "swarm-cronjob",
		Enabled: true,
	}

	if v, ok := labels["swarm.cronjob.enable"]; ok {
		integration.Enabled = v == "true"
	}

	if v, ok := labels["swarm.cronjob.schedule"]; ok {
		integration.Schedule = v
	}

	if v, ok := labels["swarm.cronjob.skip-running"]; ok {
		integration.SkipRunning = v == "true"
	}

	if v, ok := labels["swarm.cronjob.replicas"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			integration.Replicas = n
		}
	}

	return integration
}
