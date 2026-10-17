package integrations

import "strings"

// ShepherdIntegration represents parsed Shepherd auto-update configuration.
type ShepherdIntegration struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	AuthConfig string `json:"authConfig,omitempty"`
}

func detectShepherd(labels map[string]string) *ShepherdIntegration {
	var found bool

	integration := &ShepherdIntegration{Name: "shepherd"}

	for k, v := range labels {
		suffix, ok := strings.CutPrefix(k, "shepherd.")
		if !ok {
			continue
		}

		found = true

		if suffix == "auth.config" {
			integration.AuthConfig = v
		}
	}

	if !found {
		return nil
	}

	integration.Enabled = enabled(labels, "shepherd.")

	return integration
}
