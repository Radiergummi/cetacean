package sbom

// ComponentTexts holds the pool ids of one component's license and notice.
type ComponentTexts struct {
	License string `json:"license"`
	Notice  string `json:"notice,omitempty"`
}

// Artifact is the licensetexts.json document that sits beside the SBOM: a
// content-addressed pool of license and notice texts, plus the mapping from
// component identity into it. Written by scripts/licensetexts, read by this
// package — the shape is declared here so the two cannot drift.
type Artifact struct {
	Texts      map[string]string         `json:"texts"`
	Components map[string]ComponentTexts `json:"components"`
}

// ComponentKey is a component's identity in the artifact. It matches the key
// the projection deduplicates on, so the two artifacts join cleanly.
func ComponentKey(component Component) string {
	return component.Ecosystem + ":" + component.Name + "@" + component.Version
}
