package api

const jsonLDContext = "/api/context.jsonld"

// NewDetailResponse creates a JSON-LD wrapped detail response.
// Returns a map that includes @context, @id, @type, plus any extra key/value pairs.
func NewDetailResponse(id, typ string, extra map[string]any) map[string]any {
	m := make(map[string]any, len(extra)+3)
	m["@context"] = jsonLDContext
	m["@id"] = id
	m["@type"] = typ
	for k, v := range extra {
		m[k] = v
	}
	return m
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
