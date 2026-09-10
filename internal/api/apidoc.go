package api

import (
	"encoding/json"
	"net/http"

	"gopkg.in/yaml.v3"
)

const apiPlaygroundHTML = `<!DOCTYPE html>
<html>
<head><title>Cetacean API</title><meta charset="utf-8"/></head>
<body>
  <script id="api-reference" data-url="/api"></script>
  <script src="/api/scalar.js"></script>
</body>
</html>`

// HandleAPIDoc serves the API documentation. HTML requests get the Scalar
// playground; JSON requests (including default */* negotiation) get the spec
// as JSON; explicit application/yaml requests get YAML.
func HandleAPIDoc(specYAML []byte) http.HandlerFunc {
	// Convert YAML to JSON once at startup.
	var parsed any
	if err := yaml.Unmarshal(specYAML, &parsed); err != nil {
		panic("openapi spec is not valid YAML: " + err.Error())
	}
	specJSON, err := json.Marshal(convertYAMLToJSON(parsed))
	if err != nil {
		panic("openapi spec could not be converted to JSON: " + err.Error())
	}

	// Both bodies are fixed for the life of the process, so their validators
	// are hashed once here rather than on every request.
	playground := []byte(apiPlaygroundHTML)
	playgroundETag := computeETag(playground)
	specETag := computeETag(specJSON)

	return func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		w.Header().Set("Cache-Control", "public, max-age=3600")
		switch ct {
		case ContentTypeHTML:
			w.Header().Set("Content-Type", "text/html")
			w.Header().
				Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")
			writeRawWithPrecomputedETag(w, r, playground, playgroundETag)
		default:
			// JSON is the default for content negotiation (including */*).
			w.Header().Set("Content-Type", "application/json")
			writeRawWithPrecomputedETag(w, r, specJSON, specETag)
		}
	}
}

// HandleScalarJS serves the embedded Scalar API reference JavaScript bundle.
// Like the spec beside it, the bundle is fixed at build time, so its
// validator is hashed once rather than per request.
func HandleScalarJS(js []byte) http.HandlerFunc {
	etag := computeETag(js)

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		writeRawWithPrecomputedETag(w, r, js, etag)
	}
}

// convertYAMLToJSON recursively converts yaml.v3 map[string]any types to
// JSON-compatible types. Does not handle map[any]any (non-string keys).
func convertYAMLToJSON(v any) any {
	switch v := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, val := range v {
			m[k] = convertYAMLToJSON(val)
		}
		return m
	case []any:
		for i, val := range v {
			v[i] = convertYAMLToJSON(val)
		}
		return v
	default:
		return v
	}
}
