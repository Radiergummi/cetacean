// Package linkset implements the RFC 9264 JSON serialization of a set of
// links. It is pure types and marshalling, with no HTTP dependency, in the
// same spirit as api/atom and api/jsonfeed.
package linkset

import "encoding/json"

// MediaType is the content type RFC 9264 §4.2 registers for this
// serialization, and the one RFC 9727 §4.2 requires an API catalog to be
// published in.
const MediaType = "application/linkset+json"

// Document is a linkset: the set of link contexts, serialized under a single
// "linkset" member per RFC 9264 §4.2.1.
type Document struct {
	Contexts []Context `json:"linkset"`
}

// Context is one anchor together with every link that hangs off it, keyed by
// relation type.
//
// Relations are a map rather than named fields because a linkset may carry any
// registered relation type, and §4.2.1 serializes each one as a member of the
// context object named by the relation itself — so the shape of a context is
// decided by its content, not by a fixed struct.
type Context struct {
	// Anchor is the URI the links below are about. RFC 9264 §4.2.1 makes it
	// optional only for a link whose context is the linkset itself; every
	// context Cetacean publishes names one.
	Anchor string

	// Relations maps a link relation type to its targets. A relation with no
	// targets is omitted rather than serialized as an empty array.
	Relations map[string][]Target
}

// Target is one link target and the attributes describing it. RFC 9264 §4.2.1
// defines href plus the target attributes below; the ones Cetacean has no use
// for are simply not modelled.
type Target struct {
	Href string `json:"href"`

	// Type is the target's media type, the "type" target attribute. It is what
	// lets a client pick between an OpenAPI document and an HTML playground at
	// the same relation.
	Type string `json:"type,omitempty"`

	Title string `json:"title,omitempty"`
}

// MarshalJSON writes the anchor and the relations as members of one object,
// which is the shape §4.2.1 defines: a relation is named by itself, so no
// fixed struct can describe a context.
//
// Marshalling a map rather than building the object by hand is what keeps the
// bytes stable, and stability is the requirement — these documents are served
// with an ETag over them, and Go randomizes map iteration. encoding/json sorts
// map keys, so the output is deterministic without a sort here. Members
// therefore appear in lexical order, which happens to put "anchor" first for
// every relation Cetacean publishes but would not for one sorting before it.
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
