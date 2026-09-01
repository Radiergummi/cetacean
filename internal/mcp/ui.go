package mcp

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// uiResourceURIMetaKey is the flat _meta key a tool uses to name its widget.
//
// The MCP Apps SDK exposes this as RESOURCE_URI_META_KEY and its registerAppTool
// writes both this and the nested _meta.ui.resourceUri, because hosts predating
// the nested form read only this one. We do the same: emitting a single form
// hides the widget from whichever half of the ecosystem reads the other.
const uiResourceURIMetaKey = "ui/resourceUri"

// uiResource is one built widget: a self-contained HTML document served as an
// MCP Apps resource.
type uiResource struct {
	Name        string
	Title       string
	Description string
	HTML        []byte
	// PrefersBorder asks the host to draw a visible boundary around the widget.
	// Right for content that reads as a distinct panel — a table, a log tail —
	// and wrong for a graphic that should bleed into the conversation.
	PrefersBorder bool
}

// uiCSP mirrors the MCP Apps McpUiResourceCsp object. Every field is an origin
// allowlist, and an omitted one means "none": no network, no external assets,
// no nested frames. Cetacean's widgets bundle everything, so all four stay
// empty — a widget that needs an origin is a deliberate change, reviewed here.
type uiCSP struct {
	ConnectDomains  []string `json:"connectDomains,omitempty"`
	ResourceDomains []string `json:"resourceDomains,omitempty"`
	FrameDomains    []string `json:"frameDomains,omitempty"`
	BaseURIDomains  []string `json:"baseUriDomains,omitempty"`
}

// widgetCatalog describes each widget the build is expected to produce. The
// build discovers widgets by directory, so this is only the presentation copy;
// a directory with no entry here is still served, with its name as its title.
var widgetCatalog = map[string]struct {
	Title         string
	Description   string
	PrefersBorder bool
}{
	"table": {
		Title:         "Cluster resources",
		Description:   "Services in the cluster, with their state, as a sortable table.",
		PrefersBorder: true,
	},
	"topology": {
		Title:         "Cluster topology",
		Description:   "Services, overlay networks and cluster nodes as an interactive graph.",
		PrefersBorder: true,
	},
	"logs": {
		Title:         "Service logs",
		Description:   "A service's log output as a live tail, searchable and filtered by level.",
		PrefersBorder: true,
	},
}

// widgetFS holds the built widget bundles. main.go owns the //go:embed — the
// directory is outside this package — and setupMCP passes it in.
var widgetFS fs.FS

// SetWidgetFS installs the built widget bundles. Called once from main before
// the server is constructed; a nil or empty FS simply means no widgets, which
// serverExtensions reports honestly by not advertising the UI extension.
func SetWidgetFS(fsys fs.FS) { widgetFS = fsys }

// uiResourceURI is the canonical address of a widget.
func uiResourceURI(name string) string { return "ui://cetacean/" + name }

// uiResources returns every widget found in the embedded build output.
//
// Widgets are discovered rather than listed: the Vite target emits one
// directory per widget, so adding one is a directory on each side and no
// registry to keep in step. A malformed build yields no resources instead of a
// panic, and the UI extension then goes unadvertised, which is the honest
// signal — better than pointing a host at a widget that will not load.
func uiResources() []uiResource {
	if widgetFS == nil {
		return nil
	}

	entries, err := fs.ReadDir(widgetFS, ".")
	if err != nil {
		return nil
	}

	resources := make([]uiResource, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		html, err := fs.ReadFile(widgetFS, path.Join(entry.Name(), "index.html"))
		if err != nil || len(html) == 0 {
			continue
		}

		resource := uiResource{
			Name:  entry.Name(),
			Title: entry.Name(),
			HTML:  html,
		}

		if meta, ok := widgetCatalog[entry.Name()]; ok {
			resource.Title = meta.Title
			resource.Description = meta.Description
			resource.PrefersBorder = meta.PrefersBorder
		}

		resources = append(resources, resource)
	}

	// Stable order so resources/list is deterministic and its ETag holds.
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })

	return resources
}

// uiMetaFields builds the nested _meta.ui object a widget resource carries.
//
// csp is always present and always an object, never null: an absent policy and
// an empty one say different things to a host, and only the empty one states
// that this widget wants no external origin at all.
func uiMetaFields(resource uiResource) map[string]any {
	ui := map[string]any{
		"csp": uiCSP{},
	}

	if resource.PrefersBorder {
		ui["prefersBorder"] = true
	}

	return ui
}

// uiResourceMeta is the _meta a widget resource advertises in resources/list.
func uiResourceMeta(resource uiResource) *mcplib.Meta {
	return &mcplib.Meta{
		AdditionalFields: map[string]any{"ui": uiMetaFields(resource)},
	}
}

// toolUIMeta is the _meta a tool carries to say "render my result with this
// widget". Both key forms are written; see uiResourceURIMetaKey.
func toolUIMeta(widgetName string) *mcplib.Meta {
	uri := uiResourceURI(widgetName)

	return &mcplib.Meta{
		AdditionalFields: map[string]any{
			uiResourceURIMetaKey: uri,
			"ui":                 map[string]any{"resourceUri": uri},
		},
	}
}

// registerUIResources publishes each built widget at ui://cetacean/<name>.
//
// The bundle is static, so the read handler closes over the bytes rather than
// re-reading the FS per call. The contents carry the same _meta.ui as the
// listing: the listing entry is a fallback, and a host reading the resource
// takes the content's copy.
func (s *Server) registerUIResources() {
	for _, resource := range uiResources() {
		uri := uiResourceURI(resource.Name)

		listed := mcplib.NewResource(uri, resource.Name,
			mcplib.WithResourceTitle(resource.Title),
			mcplib.WithResourceDescription(resource.Description),
			mcplib.WithMIMEType(uiMIMEType),
		)
		listed.Meta = uiResourceMeta(resource)

		contents := mcplib.TextResourceContents{
			Meta:     map[string]any{"ui": uiMetaFields(resource)},
			URI:      uri,
			MIMEType: uiMIMEType,
			Text:     string(resource.HTML),
		}

		s.mcpServer.AddResource(listed, func(
			_ context.Context,
			request mcplib.ReadResourceRequest,
		) ([]mcplib.ResourceContents, error) {
			if request.Params.URI != uri {
				return nil, fmt.Errorf("unknown widget %q", request.Params.URI)
			}

			return []mcplib.ResourceContents{contents}, nil
		})
	}
}

// hasWidget reports whether the named widget was built into this binary.
func hasWidget(name string) bool {
	return slices.ContainsFunc(uiResources(), func(r uiResource) bool {
		return r.Name == name
	})
}
