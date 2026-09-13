package api

import (
	"net/http"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/linkset"
)

const apiCatalogPath = "/.well-known/api-catalog"

// oauthProtectedResourcePath is spelled again here because internal/api and
// internal/oauth deliberately do not import each other. Per RFC 9728 §3.1 a
// resource with a path takes its document beneath this one, so the bare path
// belongs to the deployment root — the web API — and /mcp has its own.
const oauthProtectedResourcePath = "/.well-known/oauth-protected-resource"

// catalogMounts is what the router mounted, which is what the catalog may
// claim. MCP is off by default, and its OAuth server is wired only when
// auth.mode is not "none" — so MCP can be reachable while the metadata
// document describing it does not exist.
type catalogMounts struct {
	mcp           bool
	oauthMetadata bool

	// apiTokens reports whether the API is offered as a protected resource. An
	// operator who turned it off means it to be undiscoverable as well as
	// unusable, so the catalog must not name a document that is not served.
	apiTokens bool
}

// protectedResourceMeta is the service-meta relation naming a resource's RFC
// 9728 document. service-meta rather than service-desc: the document describes
// the endpoint, not its interface.
func protectedResourceMeta(href string) []linkset.Target {
	return []linkset.Target{{
		Href:  href,
		Type:  "application/json",
		Title: "Protected resource metadata",
	}}
}

// HandleAPICatalog serves the RFC 9727 API catalog as an RFC 9264 linkset.
// Cetacean publishes two APIs from one process: the web API at "/", and the
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
			Title: "Cetacean web API",
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
						"service-meta": protectedResourceMeta(
							link(oauthProtectedResourcePath + "/mcp"),
						),
					},
				}}
			}
		}

		restRelations := map[string][]linkset.Target{
			// Both point at /api, which negotiates between the two; the type
			// attribute tells them apart.
			"service-desc": {{
				Href:  link("/api"),
				Type:  "application/json",
				Title: "OpenAPI description",
			}, {
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
		}

		// The API is a protected resource only when it is offered as one, and an
		// operator who turned that off means the document to be absent.
		if mounts.oauthMetadata && mounts.apiTokens {
			restRelations["service-meta"] = protectedResourceMeta(
				link(oauthProtectedResourcePath),
			)
		}

		contexts := append([]linkset.Context{
			{
				Anchor: link(apiCatalogPath),
				// item is RFC 9727 §3.1's only MUST: each names a member API.
				Relations: map[string][]linkset.Target{"item": items},
			},
			{
				Anchor:    restAPI,
				Relations: restRelations,
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
