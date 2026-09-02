package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUIResourcesAreSelfContained pins the two things an MCP Apps resource must
// be: present, and standalone. A widget is delivered as one text payload with
// no base URL, so anything it loads from a relative path is unreachable in the
// host's sandbox — a bundle that slipped back to emitting a <script src> would
// render blank with nothing in any log to say why.
func TestUIResourcesAreSelfContained(t *testing.T) {
	resources := uiResources()
	if len(resources) == 0 {
		t.Fatal("no widget resources: did `npm run build:widgets` run before `go build`?")
	}

	for _, resource := range resources {
		t.Run(resource.Name, func(t *testing.T) {
			if len(resource.HTML) == 0 {
				t.Fatal("empty widget bundle")
			}

			html := string(resource.HTML)
			for _, external := range []string{"<script src=", "<script type=\"module\" src=", "<link rel=\"stylesheet\""} {
				if strings.Contains(html, external) {
					t.Errorf("bundle references %q; a widget must inline every asset", external)
				}
			}
		})
	}
}

// TestUIResourceURIsUseTheAppsScheme — the ui:// scheme and the mcp-app profile
// are how a host tells a widget from an ordinary resource. Get either wrong and
// the resource is simply ignored.
func TestUIResourceURIsUseTheAppsScheme(t *testing.T) {
	for _, resource := range uiResources() {
		if uri := uiResourceURI(resource.Name); !strings.HasPrefix(uri, "ui://cetacean/") {
			t.Errorf("%s: URI %q must use the ui://cetacean/ scheme", resource.Name, uri)
		}
	}

	if uiMIMEType != "text/html;profile=mcp-app" {
		t.Errorf("uiMIMEType = %q, want the MCP Apps media type", uiMIMEType)
	}
}

// TestToolUIMetaCarriesBothResourceURIKeys is the wire shape mcp-go cannot
// check for us. The MCP Apps SDK reads a tool's widget from the nested
// _meta.ui.resourceUri, but older hosts read the flat _meta["ui/resourceUri"],
// and the SDK's own registerAppTool writes both. Emitting only one silently
// hides the widget from half the hosts that support it.
func TestToolUIMetaCarriesBothResourceURIKeys(t *testing.T) {
	meta := toolUIMeta("table")

	flat, ok := meta.AdditionalFields[uiResourceURIMetaKey].(string)
	if !ok {
		t.Fatalf("_meta[%q] missing, got %#v", uiResourceURIMetaKey, meta.AdditionalFields)
	}

	if want := "ui://cetacean/table"; flat != want {
		t.Errorf("_meta[%q] = %q, want %q", uiResourceURIMetaKey, flat, want)
	}

	nested, ok := meta.AdditionalFields["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing, got %#v", meta.AdditionalFields)
	}

	if got := nested["resourceUri"]; got != flat {
		t.Errorf("_meta.ui.resourceUri = %v, want %q — the two keys must agree", got, flat)
	}
}

// TestUIResourceMetaDeclaresEmptyCSP — our widgets bundle everything, so they
// need no external origin at all. An omitted csp and an empty one are not the
// same statement: the host has to be told we want nothing, so that a widget
// that later does need an origin is a deliberate, reviewable change.
func TestUIResourceMetaDeclaresEmptyCSP(t *testing.T) {
	meta := uiResourceMeta(uiResource{Name: "table"})

	ui, ok := meta.AdditionalFields["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing, got %#v", meta.AdditionalFields)
	}

	csp, ok := ui["csp"]
	if !ok {
		t.Fatal("_meta.ui.csp missing; a widget must declare its CSP explicitly")
	}

	// Serialised because the host reads JSON, and an empty Go map and a nil one
	// differ there: `{}` states "no origins", `null` states nothing.
	encoded, err := json.Marshal(csp)
	if err != nil {
		t.Fatalf("marshal csp: %v", err)
	}

	if string(encoded) == "null" {
		t.Error("csp serialises to null; it must be an object so the host sees an empty allowlist")
	}
}

// TestMain points the widget resources at the real build output. The tests
// above assert properties of the actual bundle — that it inlines its assets,
// that it is non-empty — which a synthetic fixture could not tell us. Requiring
// the build mirrors what `go build` already needs for frontend/dist.
func TestMain(m *testing.M) {
	SetWidgetFS(os.DirFS(filepath.Join("..", "..", "frontend", "dist-widgets")))

	os.Exit(m.Run())
}

// TestWidgetIsServedOverTheWire drives resources/list and resources/read
// through a real server, because everything else here tests the builders rather
// than what a host receives. Phase 2 of this upgrade shipped broken for exactly
// that reason: its unit tests exercised hooks the real transport never reaches.
func TestWidgetIsServedOverTheWire(t *testing.T) {
	handler := newTestServer(t).Handler()

	_, listed := mcpModern(t, handler, 1, "resources/list", `{}`)
	if listed.Error != nil {
		t.Fatalf("resources/list error: %+v", listed.Error)
	}

	var list struct {
		Resources []struct {
			URI      string `json:"uri"`
			Name     string `json:"name"`
			MIMEType string `json:"mimeType"`
			Meta     struct {
				UI struct {
					CSP           map[string]any `json:"csp"`
					PrefersBorder bool           `json:"prefersBorder"`
				} `json:"ui"`
			} `json:"_meta"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(listed.Result, &list); err != nil {
		t.Fatalf("decode resources/list: %v", err)
	}

	var widget *struct {
		URI      string `json:"uri"`
		Name     string `json:"name"`
		MIMEType string `json:"mimeType"`
		Meta     struct {
			UI struct {
				CSP           map[string]any `json:"csp"`
				PrefersBorder bool           `json:"prefersBorder"`
			} `json:"ui"`
		} `json:"_meta"`
	}

	for i := range list.Resources {
		if list.Resources[i].URI == "ui://cetacean/table" {
			widget = &list.Resources[i]

			break
		}
	}

	if widget == nil {
		t.Fatalf("ui://cetacean/table absent from resources/list")
	}

	if widget.MIMEType != uiMIMEType {
		t.Errorf("mimeType = %q, want %q", widget.MIMEType, uiMIMEType)
	}

	if widget.Meta.UI.CSP == nil {
		t.Error("_meta.ui.csp missing from the listing; the host cannot sandbox without it")
	}

	if !widget.Meta.UI.PrefersBorder {
		t.Error("_meta.ui.prefersBorder missing; the table widget asks for a border")
	}

	// The document itself must come back readable, with its own _meta — a
	// content item's copy takes precedence over the listing's for the host.
	_, read := mcpModern(t, handler, 2, "resources/read",
		`{"uri":"ui://cetacean/table"}`)
	if read.Error != nil {
		t.Fatalf("resources/read error: %+v", read.Error)
	}

	var contents struct {
		Contents []struct {
			URI      string         `json:"uri"`
			MIMEType string         `json:"mimeType"`
			Text     string         `json:"text"`
			Meta     map[string]any `json:"_meta"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(read.Result, &contents); err != nil {
		t.Fatalf("decode resources/read: %v", err)
	}

	if len(contents.Contents) != 1 {
		t.Fatalf("got %d content items, want 1", len(contents.Contents))
	}

	item := contents.Contents[0]
	if item.MIMEType != uiMIMEType {
		t.Errorf("content mimeType = %q, want %q", item.MIMEType, uiMIMEType)
	}

	if !strings.Contains(item.Text, "<!doctype html>") &&
		!strings.Contains(item.Text, "<!DOCTYPE html>") {
		t.Errorf("content is not an HTML document: %.120s", item.Text)
	}

	if _, ok := item.Meta["ui"]; !ok {
		t.Error("content _meta.ui missing; it is the copy the host actually honours")
	}
}
