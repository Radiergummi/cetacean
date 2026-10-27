package integrations

import (
	"strconv"
	"strings"
)

// GantryIntegration represents parsed Gantry service update configuration.
// The pointer fields override a Gantry-wide default, so nil means unset.
type GantryIntegration struct {
	Name                 string `json:"name"`
	Enabled              bool   `json:"enabled"`
	AuthConfig           string `json:"authConfig,omitempty"`
	ManifestCmd          string `json:"manifestCmd,omitempty"`
	ManifestOptions      string `json:"manifestOptions,omitempty"`
	RollbackOnFailure    *bool  `json:"rollbackOnFailure,omitempty"`
	RollbackOptions      string `json:"rollbackOptions,omitempty"`
	UpdateJobs           *bool  `json:"updateJobs,omitempty"`
	UpdateOptions        string `json:"updateOptions,omitempty"`
	UpdateTimeoutSeconds *int   `json:"updateTimeoutSeconds,omitempty"`
}

func detectGantry(labels map[string]string) *GantryIntegration {
	var found bool

	integration := &GantryIntegration{Name: "gantry", Enabled: true}

	for k, v := range labels {
		suffix, ok := strings.CutPrefix(k, "gantry.")
		if !ok {
			continue
		}

		found = true

		switch suffix {
		case "services.excluded":
			integration.Enabled = v != "true"
		case "auth.config":
			integration.AuthConfig = v
		case "manifest.cmd":
			integration.ManifestCmd = v
		case "manifest.options":
			integration.ManifestOptions = v
		case "rollback.on_failure":
			integration.RollbackOnFailure = new(v == "true")
		case "rollback.options":
			integration.RollbackOptions = v
		case "update.jobs":
			integration.UpdateJobs = new(v == "true")
		case "update.options":
			integration.UpdateOptions = v
		case "update.timeout_seconds":
			if n, err := strconv.Atoi(v); err == nil {
				integration.UpdateTimeoutSeconds = new(n)
			}
		}
	}

	if !found {
		return nil
	}

	return integration
}
