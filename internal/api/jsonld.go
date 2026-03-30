package api

import (
	"context"
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
	keys := make([]string, 0, len(d.extra))
	for k := range d.extra {
		keys = append(keys, k)
	}
	slices.Sort(keys)

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

	for _, k := range keys {
		buf = append(buf, ',')
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf = append(buf, key...)
		buf = append(buf, ':')
		val, err := json.Marshal(d.extra[k])
		if err != nil {
			return nil, err
		}
		buf = append(buf, val...)
	}

	buf = append(buf, '}')
	return buf, nil
}

// NewDetailResponse creates a JSON-LD wrapped detail response with
// deterministic serialization order.
func NewDetailResponse(ctx context.Context, id, typ string, extra map[string]any) DetailResponse {
	return DetailResponse{
		context: absPath(ctx, jsonLDContext),
		id:      absPath(ctx, id),
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
