package api

import (
	"context"
	"slices"

	json "github.com/goccy/go-json"
)

const jsonLDContext = "/api/context.jsonld"

// DetailResponse is a JSON-LD wrapped detail response with deterministic
// key ordering. The @context, @id, @type fields are serialized first,
// followed by extra fields.
//
// extra may be a map[string]any (keys sorted alphabetically) or a struct
// (fields serialized in declaration order via JSON tags). Both produce
// deterministic output suitable for stable ETags.
type DetailResponse struct {
	context string
	id      string
	typ     string
	extra   any
}

// MarshalJSON produces deterministic output: @context, @id, @type first,
// then extra fields.
func (d DetailResponse) MarshalJSON() ([]byte, error) {
	idJSON, err := json.Marshal(d.id)
	if err != nil {
		return nil, err
	}

	typJSON, err := json.Marshal(d.typ)
	if err != nil {
		return nil, err
	}

	// Estimate capacity: fixed fields ~80 bytes + extras.
	buf := make([]byte, 0, 256)
	buf = append(buf, `{"@context":"`...)
	buf = append(buf, d.context...)
	buf = append(buf, `","@id":`...)
	buf = append(buf, idJSON...)
	buf = append(buf, `,"@type":`...)
	buf = append(buf, typJSON...)

	switch extra := d.extra.(type) {
	case map[string]any:
		keys := make([]string, 0, len(extra))
		for k := range extra {
			keys = append(keys, k)
		}
		slices.Sort(keys)

		for _, k := range keys {
			buf = append(buf, ',')

			key, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}

			buf = append(buf, key...)
			buf = append(buf, ':')

			val, err := json.Marshal(extra[k])
			if err != nil {
				return nil, err
			}

			buf = append(buf, val...)
		}

	case nil:
		// No extra fields.

	default:
		// Struct: marshal and splice its fields after the @-prefixed keys.
		extraBytes, err := json.Marshal(extra)
		if err != nil {
			return nil, err
		}

		if len(extraBytes) > 2 { // not just "{}"
			buf = append(buf, ',')
			buf = append(buf, extraBytes[1:len(extraBytes)-1]...)
		}
	}

	buf = append(buf, '}')
	return buf, nil
}

// NewDetailResponse creates a JSON-LD wrapped detail response.
// extra may be a map[string]any or a struct with JSON tags.
func NewDetailResponse(ctx context.Context, id, typ string, extra any) DetailResponse {
	return DetailResponse{
		context: absPath(ctx, jsonLDContext),
		id:      absPath(ctx, id),
		typ:     typ,
		extra:   extra,
	}
}

// Item wraps a collection entry with JSON-LD @id and @type fields.
// MarshalJSON produces: {"@id": "...", "@type": "...", <fields of T>}
type Item[T any] struct {
	id  string
	typ string
	val T
}

// MarshalJSON inlines the wrapped value's fields after @id and @type.
func (i Item[T]) MarshalJSON() ([]byte, error) {
	valBytes, err := json.Marshal(i.val)
	if err != nil {
		return nil, err
	}

	idJSON, err := json.Marshal(i.id)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 0, len(valBytes)+64)
	buf = append(buf, `{"@id":`...)
	buf = append(buf, idJSON...)
	buf = append(buf, `,"@type":"`...)
	buf = append(buf, i.typ...)
	buf = append(buf, `"`...)

	if len(valBytes) > 2 { // not "{}"
		buf = append(buf, ',')
		buf = append(buf, valBytes[1:len(valBytes)-1]...)
	}

	buf = append(buf, '}')
	return buf, nil
}

// wrapItems maps a slice of T to []Item[T] using the given type name and ID extractor.
func wrapItems[T any](items []T, typ string, id func(T) string) []Item[T] {
	out := make([]Item[T], len(items))
	for index, v := range items {
		out[index] = Item[T]{id: id(v), typ: typ, val: v}
	}
	return out
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
func NewCollectionResponse[T any](
	ctx context.Context, items []T, total, limit, offset int,
) CollectionResponse[T] {
	return CollectionResponse[T]{
		Context: absPath(ctx, jsonLDContext),
		Type:    "Collection",
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	}
}
