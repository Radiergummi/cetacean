package integrations

import "strings"

// ShepherdIntegration represents parsed Shepherd auto-update configuration.
type ShepherdIntegration struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	AuthConfig string `json:"authConfig,omitempty"`
}

func detectShepherd(labels map[string]string) *ShepherdIntegration {
	var (
		found     bool
		enableSet bool
		enableVal string
	)

	integration := &ShepherdIntegration{Name: "shepherd"}

	for k, v := range labels {
		suffix, ok := strings.CutPrefix(k, "shepherd.")
		if !ok {
			continue
		}

		found = true

		switch suffix {
		case "enable":
			enableSet = true
			enableVal = v
		case "auth.config":
			integration.AuthConfig = v
		}
	}

	if !found {
		return nil
	}

	integration.Enabled = !enableSet || enableVal == "true"

	return integration
}
