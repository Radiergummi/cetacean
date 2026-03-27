package integrations

import "strings"

// Result holds detected integrations for a service.
type Result struct {
	Integrations []any             `json:"integrations,omitempty"`
	Remaining    map[string]string `json:"-"`
}

// Detect runs all registered detectors against the given labels.
// Returns detected integrations and the remaining unconsumed labels.
func Detect(labels map[string]string) Result {
	remaining := make(map[string]string, len(labels))
	for k, v := range labels {
		remaining[k] = v
	}

	var integrations []any

	if t := detectTraefik(labels); t != nil {
		integrations = append(integrations, t)
		for k := range labels {
			if strings.HasPrefix(k, "traefik.") {
				delete(remaining, k)
			}
		}
	}

	return Result{
		Integrations: integrations,
		Remaining:    remaining,
	}
}
