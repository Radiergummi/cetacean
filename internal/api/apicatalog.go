package api

import (
	"net/http"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/linkset"
)

const apiCatalogPath = "/.well-known/api-catalog"

// oauthProtectedResourcePath is spelled again here because internal/api and
// internal/mcp deliberately do not import each other.
const oauthProtectedResourcePath = "/.well-known/oauth-protected-resource"

// catalogMounts is what the router mounted, which is what the catalog may
// claim. MCP is off by default, and its OAuth server is wired only when
// auth.mode is not "none" — so MCP can be reachable while the metadata
// document describing it does not exist.
type catalogMounts struct {
	mcp           bool
	oauthMetadata bool
}

// HandleAPICatalog serves the RFC 9727 API catalog as an RFC 9264 linkset.
// Cetacean publishes two APIs from one process: the REST API at "/", and the
// MCP server at /mcp when enabled.
//
// The document is unauthenticated — /.well-known/ is exempt — so it may name
// only public resources.
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

			if mounts.oauthMetadata {
				mcpContexts = []linkset.Context{{
					Anchor: mcpAPI,
					Relations: map[string][]linkset.Target{
						// service-meta, not service-desc: RFC 9728 metadata
						// describes the endpoint, not its interface, and MCP
						// serves no interface description.
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
				// item is RFC 9727 §3.1's only MUST: each names a member API.
				Relations: map[string][]linkset.Target{"item": items},
			},
			{
				Anchor: restAPI,
				Relations: map[string][]linkset.Target{
					// Both point at /api, which negotiates between the two;
					// the type attribute tells them apart.
					"service-desc": {{
						Href:  link("/api"),
						Type:  "application/json",
						Title: "OpenAPI description",
					}, {
						// The base type, without the version parameter:
						// TestAPICatalogTargetsAnswerAsAdvertised compares a
						// target's type against the response's, cut at the
						// first ";".
						Href:  link(asyncAPIPath),
						Type:  asyncAPIMediaTypeBase,
						Title: "AsyncAPI description of the event streams",
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
