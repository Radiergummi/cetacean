package api

import (
	"net/http"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/linkset"
)

// apiCatalogPath is where RFC 9727 mints the catalog, and the value of the
// api-catalog link relation discoveryLinks puts on every response.
const apiCatalogPath = "/.well-known/api-catalog"

// catalogMounts is what the router actually mounted, which is what the catalog
// is allowed to claim. A catalog is a promise that what it lists is there, and
// both of these are optional at runtime: MCP is off by default, and its OAuth
// authorization server is wired only when an auth mode other than "none" is
// configured — so MCP can be reachable while the metadata document describing
// it does not exist.
type catalogMounts struct {
	mcp           bool
	oauthMetadata bool
}

// HandleAPICatalog serves the RFC 9727 API catalog as an RFC 9264 linkset.
//
// Cetacean publishes two APIs from one process: the REST API rooted at "/",
// described by OpenAPI, and the MCP server at /mcp, which speaks a different
// protocol behind its own authorization. A catalog is how a client learns that
// without being told, which is the only reason this document says more than
// the discovery Link headers already do.
//
// The document is unauthenticated — /.well-known/ is exempt from auth — and so
// may name only resources that are themselves public. Every URI below is a
// description document or a health probe; none of them is a cluster resource.
func HandleAPICatalog(mounts catalogMounts) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		restAPI := absURL(r, "/")

		items := []linkset.Target{{
			Href:  restAPI,
			Title: "Cetacean REST API",
		}}

		contexts := []linkset.Context{
			{
				Anchor: absURL(r, apiCatalogPath),
			},
			{
				Anchor: restAPI,
				Relations: map[string][]linkset.Target{
					// Both relations point at /api: it is one resource that
					// content-negotiates between the OpenAPI document and the
					// Scalar playground, and the type attribute is what tells
					// the two apart.
					"service-desc": {{
						Href:  absURL(r, "/api"),
						Type:  "application/json",
						Title: "OpenAPI description",
					}},
					"service-doc": {{
						Href:  absURL(r, "/api"),
						Type:  "text/html",
						Title: "API reference",
					}},
					"describedby": {{
						Href:  absURL(r, "/api/context.jsonld"),
						Type:  "application/ld+json",
						Title: "JSON-LD context",
					}},
					"status": {{
						Href:  absURL(r, "/-/health"),
						Type:  "application/json",
						Title: "Health",
					}},
				},
			},
		}

		if mounts.mcp {
			mcpAPI := absURL(r, "/mcp")

			items = append(items, linkset.Target{
				Href:  mcpAPI,
				Title: "Cetacean MCP server",
			})

			// With no authorization server there is nothing further to say
			// about /mcp, and a context carrying an anchor and no links says
			// exactly that at greater length. The item above still announces
			// the API.
			if mounts.oauthMetadata {
				contexts = append(contexts, linkset.Context{
					Anchor: mcpAPI,
					Relations: map[string][]linkset.Target{
						// RFC 9728 protected resource metadata is metadata
						// about the endpoint rather than a description of its
						// interface, which is service-meta's distinction from
						// service-desc in RFC 8631. MCP has no served
						// interface description: this server is stateless and
						// answers no initialize, so its capabilities are not
						// discoverable ahead of a call.
						"service-meta": {{
							Href:  absURL(r, "/.well-known/oauth-protected-resource"),
							Type:  "application/json",
							Title: "Protected resource metadata",
						}},
					},
				})
			}
		}

		// The catalog's own context carries the item links, which is the one
		// relation RFC 9727 §3.1 requires: each names an API that is a member
		// of this catalog.
		contexts[0].Relations = map[string][]linkset.Target{"item": items}

		body, err := json.Marshal(linkset.Document{Contexts: contexts})
		if err != nil {
			writeProblem(w, r, http.StatusInternalServerError,
				"could not build the API catalog")

			return
		}

		w.Header().Set("Content-Type", linkset.MediaType)
		writeRawWithETag(w, r, body)
	}
}
