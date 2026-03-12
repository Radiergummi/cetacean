package api

import (
	"slices"

	json "github.com/goccy/go-json"
)

const jsonLDContext = "/api/context.jsonld"

// DetailResponse is a JSON-LD wrapped detail response with deterministic
// key ordering. The @context, @id, @type fields are serialized first,
// followed by extra fields in sorted key order. This guarantees stable
// ETags regardless of the JSON library's map iteration order.
type DetailResponse struct {
	context string
	id      string
	typ     string
	extra   map[string]any
}

// MarshalJSON produces deterministic output: @context, @id, @type first,
// then extra keys in sorted order.
func (d DetailResponse) MarshalJSON() ([]byte, error) {
	// Pre-allocate: 3 fixed keys + extras.
	keys := make([]string, 0, len(d.extra))
	for k := range d.extra {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	// Build ordered key-value pairs.
	ordered := make([]orderedKV, 0, 3+len(keys))
	ordered = append(ordered,
		orderedKV{"@context", d.context},
		orderedKV{"@id", d.id},
		orderedKV{"@type", d.typ},
	)
	for _, k := range keys {
		ordered = append(ordered, orderedKV{k, d.extra[k]})
	}
	return marshalOrdered(ordered)
}

type orderedKV struct {
	key string
	val any
}

// marshalOrdered serializes key-value pairs as a JSON object in the given order.
func marshalOrdered(pairs []orderedKV) ([]byte, error) {
	buf := []byte{'{'}
	for i, kv := range pairs {
		if i > 0 {
			buf = append(buf, ',')
		}
		key, err := json.Marshal(kv.key)
		if err != nil {
			return nil, err
		}
		val, err := json.Marshal(kv.val)
		if err != nil {
			return nil, err
		}
		buf = append(buf, key...)
		buf = append(buf, ':')
		buf = append(buf, val...)
	}
	buf = append(buf, '}')
	return buf, nil
}

// NewDetailResponse creates a JSON-LD wrapped detail response with
// deterministic serialization order.
func NewDetailResponse(id, typ string, extra map[string]any) DetailResponse {
	return DetailResponse{
		context: jsonLDContext,
		id:      id,
		typ:     typ,
		extra:   extra,
	}
}

// CollectionResponse is the JSON-LD wrapper for list endpoints.
type CollectionResponse[T any] struct {
	Context string `json:"@context"`
	Type    string `json:"@type"`
	Items   []T    `json:"items"`
	Total   int    `json:"total"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

// NewCollectionResponse creates a CollectionResponse with JSON-LD metadata.
func NewCollectionResponse[T any](items []T, total, limit, offset int) CollectionResponse[T] {
	return CollectionResponse[T]{
		Context: jsonLDContext,
		Type:    "Collection",
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	}
}
