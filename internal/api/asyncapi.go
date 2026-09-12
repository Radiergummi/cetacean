package api

import (
	"maps"
	"net/http"
	"sync/atomic"

	json "github.com/goccy/go-json"
)

const (
	asyncAPIPath = "/api/asyncapi"

	// asyncAPIMediaTypeBase is the type without its version parameter, for
	// the places that compare types rather than state them.
	asyncAPIMediaTypeBase = "application/vnd.aai.asyncapi+json"
	asyncAPIMediaType     = asyncAPIMediaTypeBase + ";version=3.0.0"
)

// asyncAPIRendering is the document as one origin sees it, under the key that
// produced it.
type asyncAPIRendering struct {
	key  string
	body *staticBody
}

// HandleAsyncAPI serves the AsyncAPI description of the SSE streams.
//
// One representation, served unconditionally, as HandleContext does for the
// JSON-LD context: this document has exactly one form, so there is nothing to
// negotiate and no SPA fallback to reach.
//
// AsyncAPI 3.0 requires host on a server object, so the document names the
// deployment's own origin — from the validated origin, never from a raw
// forwarding header. The body is therefore a pure function of scheme, host and
// base path, and one rendering is retained under that key so this document is
// hashed and compressed once like every other served document. A single slot
// rather than a map: origin falls back to r.Host when server.public_url is
// unset, so a hostile client can vary the key without bound — a map would
// grow, a slot degrades to rebuilding per request and no worse.
func HandleAsyncAPI(specYAML []byte) http.HandlerFunc {
	doc, err := yamlDocument(specYAML)
	if err != nil {
		panic("asyncapi spec " + err.Error())
	}

	var current atomic.Pointer[asyncAPIRendering]

	return func(w http.ResponseWriter, r *http.Request) {
		scheme, host := origin(r)
		base := BasePathFromContext(r.Context())
		key := scheme + "\x00" + host + "\x00" + base

		rendering := current.Load()

		if rendering == nil || rendering.key != key {
			body, err := renderAsyncAPI(doc, scheme, host, base)
			if err != nil {
				writeErrorCode(w, r, "API009", "failed to serialize response")

				return
			}

			rendering = &asyncAPIRendering{key: key, body: newStaticBody(body)}
			current.Store(rendering)
		}

		w.Header().Set("Content-Type", asyncAPIMediaType)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		rendering.body.serve(w, r)
	}
}

// renderAsyncAPI marshals doc with the server block one origin gets.
func renderAsyncAPI(doc map[string]any, scheme, host, base string) ([]byte, error) {
	server := map[string]any{
		"host":        host,
		"protocol":    scheme,
		"description": "This deployment.",
	}

	if base != "" {
		server["pathname"] = base
	}

	// Copied shallowly so a concurrent render cannot see this server block;
	// nothing below servers is written.
	described := make(map[string]any, len(doc))
	maps.Copy(described, doc)

	described["servers"] = map[string]any{"self": server}

	return json.Marshal(described)
}
