package api

import (
	"maps"
	"net/http"

	json "github.com/goccy/go-json"
	"gopkg.in/yaml.v3"
)

const (
	asyncAPIPath = "/api/asyncapi"

	// asyncAPIMediaTypeBase is the type without its version parameter, for
	// the places that compare types rather than state them.
	asyncAPIMediaTypeBase = "application/vnd.aai.asyncapi+json"
	asyncAPIMediaType     = asyncAPIMediaTypeBase + ";version=3.0.0"
)

// HandleAsyncAPI serves the AsyncAPI description of the SSE streams.
//
// One representation, served unconditionally, as HandleContext does for the
// JSON-LD context: this document has exactly one form, so there is nothing to
// negotiate and no SPA fallback to reach.
//
// The body is rebuilt per request because AsyncAPI 3.0 requires host on a
// server object, so the document names the deployment's own origin. That
// value comes from the validated origin, never from a raw forwarding header.
func HandleAsyncAPI(specYAML []byte) http.HandlerFunc {
	var parsed any
	if err := yaml.Unmarshal(specYAML, &parsed); err != nil {
		panic("asyncapi spec is not valid YAML: " + err.Error())
	}

	doc, ok := convertYAMLToJSON(parsed).(map[string]any)
	if !ok {
		panic("asyncapi spec does not parse to an object")
	}

	return func(w http.ResponseWriter, r *http.Request) {
		scheme, host := origin(r)

		server := map[string]any{
			"host":        host,
			"protocol":    scheme,
			"description": "This deployment.",
		}

		if base := BasePathFromContext(r.Context()); base != "" {
			server["pathname"] = base
		}

		// Copied shallowly so concurrent requests cannot see each other's
		// server block; nothing below servers is written.
		described := make(map[string]any, len(doc))
		maps.Copy(described, doc)

		described["servers"] = map[string]any{"self": server}

		body, err := json.Marshal(described)
		if err != nil {
			writeErrorCode(w, r, "API009", "failed to serialize response")

			return
		}

		w.Header().Set("Content-Type", asyncAPIMediaType)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		writeRawWithETag(w, r, body)
	}
}
