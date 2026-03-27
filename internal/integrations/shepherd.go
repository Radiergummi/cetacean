package integrations

import "strings"

// ShepherdIntegration represents parsed Shepherd auto-update configuration.
type ShepherdIntegration struct {
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Schedule    string `json:"schedule,omitempty"`
	ImageFilter string `json:"imageFilter,omitempty"`
	Latest      bool   `json:"latest,omitempty"`
	UpdateOpts  string `json:"updateOpts,omitempty"`
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
		case "schedule":
			integration.Schedule = v
		case "image-filter":
			integration.ImageFilter = v
		case "latest":
			integration.Latest = v == "true"
		case "update-opts":
			integration.UpdateOpts = v
		}
	}

	if !found {
		return nil
	}

	integration.Enabled = !enableSet || enableVal == "true"

	return integration
}
