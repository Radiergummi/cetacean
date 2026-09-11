// Package linkset implements the RFC 9264 JSON serialization of a set of
// links. It is pure types and marshalling, with no HTTP dependency, in the
// same spirit as api/atom and api/jsonfeed.
package linkset

import (
	"bytes"
	"encoding/json"
	"sort"
)

// MediaType is the content type RFC 9264 §4.2 registers for this
// serialization, and the one RFC 9727 §4.2 requires an API catalog to be
// published in.
const MediaType = "application/linkset+json"

// Document is a linkset: the set of link contexts, serialized under a single
// "linkset" member per RFC 9264 §4.2.1.
type Document struct {
	Contexts []Context
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

// MarshalJSON writes the document as {"linkset": [...]}.
func (d Document) MarshalJSON() ([]byte, error) {
	contexts := d.Contexts
	if contexts == nil {
		contexts = []Context{}
	}

	return json.Marshal(struct {
		Linkset []Context `json:"linkset"`
	}{Linkset: contexts})
}

// MarshalJSON writes the anchor first and then each relation, in sorted order.
//
// The ordering is not cosmetic: these documents are served with an ETag over
// their bytes, and Go randomizes map iteration, so an unsorted context would
// hash differently on every request and no conditional GET would ever match.
func (c Context) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteByte('{')

	if c.Anchor != "" {
		anchor, err := json.Marshal(c.Anchor)
		if err != nil {
			return nil, err
		}

		buf.WriteString(`"anchor":`)
		buf.Write(anchor)
	}

	relations := make([]string, 0, len(c.Relations))

	for relation, targets := range c.Relations {
		if len(targets) == 0 {
			continue
		}

		relations = append(relations, relation)
	}

	sort.Strings(relations)

	for _, relation := range relations {
		if buf.Len() > 1 {
			buf.WriteByte(',')
		}

		name, err := json.Marshal(relation)
		if err != nil {
			return nil, err
		}

		targets, err := json.Marshal(c.Relations[relation])
		if err != nil {
			return nil, err
		}

		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(targets)
	}

	buf.WriteByte('}')

	return buf.Bytes(), nil
}
