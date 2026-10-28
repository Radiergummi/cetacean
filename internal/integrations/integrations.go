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

	if c := detectCaddy(labels); c != nil {
		integrations = append(integrations, c)
	}

	if d := detectDocoCD(labels); d != nil {
		integrations = append(integrations, d)
	}

	if g := detectGantry(labels); g != nil {
		integrations = append(integrations, g)
	}

	if d := detectDeploymentAction(labels); d != nil {
		integrations = append(integrations, d)
	}

	return integrations
}

// enabled reads an integration's <prefix>enable label: present but anything
// other than "true" disables it, and absent leaves it enabled.
func enabled(labels map[string]string, prefix string) bool {
	v, ok := labels[prefix+"enable"]
	return !ok || v == "true"
}
