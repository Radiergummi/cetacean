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
	var found bool
	for k := range labels {
		if strings.HasPrefix(k, "shepherd.") {
			found = true
			break
		}
	}

	if !found {
		return nil
	}

	integration := &ShepherdIntegration{
		Name:    "shepherd",
		Enabled: true,
	}

	if v, ok := labels["shepherd.enable"]; ok {
		integration.Enabled = v == "true"
	}

	if v, ok := labels["shepherd.schedule"]; ok {
		integration.Schedule = v
	}

	if v, ok := labels["shepherd.image-filter"]; ok {
		integration.ImageFilter = v
	}

	if v, ok := labels["shepherd.latest"]; ok {
		integration.Latest = v == "true"
	}

	if v, ok := labels["shepherd.update-opts"]; ok {
		integration.UpdateOpts = v
	}

	return integration
}
