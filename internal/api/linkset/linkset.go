// Package linkset implements the RFC 9264 JSON serialization of a set of
// links, as pure types with no HTTP dependency — like api/atom and
// api/jsonfeed.
package linkset

import "encoding/json"

// MediaType is what RFC 9264 §4.2 registers, and the only type RFC 9727 §4.2
// permits for an API catalog.
const MediaType = "application/linkset+json"

// Document is a linkset.
type Document struct {
	Contexts []Context `json:"linkset"`
}

// Context is one anchor and every link hanging off it. Relations are a map
// because §4.2.1 names each member by its relation type, so no fixed struct
// can describe one.
type Context struct {
	Anchor    string
	Relations map[string][]Target
}

// Target is one link target. §4.2.1 defines further target attributes; the
// ones Cetacean has no use for are not modelled.
type Target struct {
	Href  string `json:"href"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
}

// MarshalJSON emits the anchor and the relations as members of one object.
// Marshalling a map is what keeps the bytes stable — encoding/json sorts map
// keys, and these documents carry an ETag over them.
func (c Context) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(c.Relations)+1)

	if c.Anchor != "" {
		out["anchor"] = c.Anchor
	}

	for relation, targets := range c.Relations {
		if len(targets) == 0 {
			continue
		}

		out[relation] = targets
	}

	return json.Marshal(out)
}
