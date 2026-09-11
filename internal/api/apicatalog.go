package api

import (
	"net/http"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/linkset"
)

// apiCatalogPath is where RFC 9727 mints the catalog, and the value of the
// api-catalog link relation discoveryLinks puts on every response.
const apiCatalogPath = "/.well-known/api-catalog"

// oauthProtectedResourcePath is where internal/mcp/oauth registers the RFC
// 9728 metadata document. Spelled again here because internal/api and
// internal/mcp deliberately do not import each other; catalogMounts.oauthMetadata
// is what keeps this from being claimed when that route is not mounted.
const oauthProtectedResourcePath = "/.well-known/oauth-protected-resource"

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
		ctx := r.Context()
		origin := originOf(r)

		link := func(path string) string { return origin + absPath(ctx, path) }

		restAPI := link("/")

		items := []linkset.Target{{
			Href:  restAPI,
			Title: "Cetacean REST API",
		}}

		var mcpContexts []linkset.Context

		if mounts.mcp {
			mcpAPI := link("/mcp")

			items = append(items, linkset.Target{
				Href:  mcpAPI,
				Title: "Cetacean MCP server",
			})

			// With no authorization server there is nothing further to say
			// about /mcp, and a context carrying an anchor and no links says
			// exactly that at greater length. The item above still announces
			// the API.
			if mounts.oauthMetadata {
				mcpContexts = []linkset.Context{{
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
							Href:  link(oauthProtectedResourcePath),
							Type:  "application/json",
							Title: "Protected resource metadata",
						}},
					},
				}}
			}
		}

		contexts := append([]linkset.Context{
			{
				Anchor: link(apiCatalogPath),
				// item is the one relation RFC 9727 §3.1 requires: each names
				// an API that is a member of this catalog.
				Relations: map[string][]linkset.Target{"item": items},
			},
			{
				Anchor: restAPI,
				Relations: map[string][]linkset.Target{
					// Both relations point at /api: it is one resource that
					// content-negotiates between the OpenAPI document and the
					// Scalar playground, and the type attribute is what tells
					// the two apart.
					"service-desc": {{
						Href:  link("/api"),
						Type:  "application/json",
						Title: "OpenAPI description",
					}},
					"service-doc": {{
						Href:  link("/api"),
						Type:  "text/html",
						Title: "API reference",
					}},
					"describedby": {{
						Href:  link(jsonLDContext),
						Type:  "application/ld+json",
						Title: "JSON-LD context",
					}},
					"status": {{
						Href:  link("/-/health"),
						Type:  "application/json",
						Title: "Health",
					}},
				},
			},
		}, mcpContexts...)

		body, err := json.Marshal(linkset.Document{Contexts: contexts})
		if err != nil {
			writeErrorCode(w, r, "API009", "failed to serialize response")

			return
		}

		w.Header().Set("Content-Type", linkset.MediaType)
		writeRawWithETag(w, r, body)
	}
}
