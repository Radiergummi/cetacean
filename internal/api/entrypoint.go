package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/version"
)

// entrypointResources are the collections the entry point names, under the
// term a client looks each one up by. Nothing derives this from the mux: it
// records every pattern, and most of them are not a way into the API.
var entrypointResources = []string{
	"cluster", "configs", "disk-usage", "events", "history", "metrics",
	"networks", "nodes", "plugins", "recommendations", "search", "secrets",
	"services", "stacks", "swarm", "tasks", "topology", "volumes",
}

// HandleEntrypoint serves the document a client that knows only the origin
// reads to find everything else. The RFC 9727 catalog anchors the web API
// here, so this is the resource it has been pointing at all along.
func HandleEntrypoint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// A node reference rather than a bare string: under @vocab a string is a
	// literal, and these are links.
	resources := make(map[string]any, len(entrypointResources))
	for _, name := range entrypointResources {
		resources[name] = map[string]string{"@id": absPath(ctx, "/"+name)}
	}

	writeCachedJSON(w, r, NewDetailResponse(ctx, "/", "EntryPoint", map[string]any{
		"name":      "Cetacean",
		"version":   version.Version,
		"resources": resources,
	}))
}
