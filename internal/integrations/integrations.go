package integrations

// Detect runs all registered detectors against the given labels.
// Returns detected integrations, or nil if none are found.
func Detect(labels map[string]string) []any {
	var integrations []any

	if t := detectTraefik(labels); t != nil {
		integrations = append(integrations, t)
	}

	if s := detectShepherd(labels); s != nil {
		integrations = append(integrations, s)
	}

	if c := detectCronjob(labels); c != nil {
		integrations = append(integrations, c)
	}

	if d := detectDiun(labels); d != nil {
		integrations = append(integrations, d)
	}

	return integrations
}
