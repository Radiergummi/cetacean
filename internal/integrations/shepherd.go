package integrations

import (
	"slices"
	"strings"
)

// ShepherdIntegration represents parsed Shepherd auto-update configuration.
type ShepherdIntegration struct {
	Name       string   `json:"name"`
	Enabled    bool     `json:"enabled"`
	Labels     []string `json:"labels"`
	AuthConfig string   `json:"authConfig,omitempty"`
}

func detectShepherd(labels map[string]string) *ShepherdIntegration {
	integration := &ShepherdIntegration{Name: "shepherd"}

	for k, v := range labels {
		suffix, ok := strings.CutPrefix(k, "shepherd.")
		if !ok {
			continue
		}

		integration.Labels = append(integration.Labels, k)

		if suffix == "auth.config" {
			integration.AuthConfig = v
		}
	}

	if len(integration.Labels) == 0 {
		return nil
	}

	slices.Sort(integration.Labels)

	integration.Enabled = enabled(labels, "shepherd.")

	return integration
}
