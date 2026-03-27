package integrations

import (
	"strconv"
	"strings"
)

// DiunIntegration represents parsed Diun image update notifier configuration.
type DiunIntegration struct {
	Name        string            `json:"name"`
	Enabled     bool              `json:"enabled"`
	WatchRepo   bool              `json:"watchRepo,omitempty"`
	NotifyOn    string            `json:"notifyOn,omitempty"`
	MaxTags     int               `json:"maxTags,omitempty"`
	IncludeTags string            `json:"includeTags,omitempty"`
	ExcludeTags string            `json:"excludeTags,omitempty"`
	SortTags    string            `json:"sortTags,omitempty"`
	RegOpt      string            `json:"regopt,omitempty"`
	HubLink     string            `json:"hubLink,omitempty"`
	Platform    string            `json:"platform,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

func detectDiun(labels map[string]string) *DiunIntegration {
	var found bool
	var enableVal string
	var enableSet bool

	integration := &DiunIntegration{Name: "diun"}

	for k, v := range labels {
		suffix, ok := strings.CutPrefix(k, "diun.")
		if !ok {
			continue
		}

		found = true

		switch suffix {
		case "enable":
			enableSet = true
			enableVal = v
		case "watch_repo":
			integration.WatchRepo = v == "true"
		case "notify_on":
			integration.NotifyOn = v
		case "max_tags":
			if n, err := strconv.Atoi(v); err == nil {
				integration.MaxTags = n
			}
		case "include_tags":
			integration.IncludeTags = v
		case "exclude_tags":
			integration.ExcludeTags = v
		case "sort_tags":
			integration.SortTags = v
		case "regopt":
			integration.RegOpt = v
		case "hub_link":
			integration.HubLink = v
		case "platform":
			integration.Platform = v
		default:
			if metaKey, ok := strings.CutPrefix(suffix, "metadata."); ok {
				if integration.Metadata == nil {
					integration.Metadata = make(map[string]string)
				}
				integration.Metadata[metaKey] = v
			}
		}
	}

	if !found {
		return nil
	}

	integration.Enabled = !enableSet || enableVal == "true"

	return integration
}
