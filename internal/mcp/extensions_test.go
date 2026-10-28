package mcp

import (
	"encoding/json"
	"regexp"
	"testing"
	"testing/fstest"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/spec"
)

// Holds the advertisement and the capability together: if server/discover names
// the Tasks extension, tasks/get must work. The check is behavioural on purpose,
// since mcp-go gates every tasks/* method on a separate option from the one that
// advertises — so a host could read the extension and be told the method is gone.
func TestTasksExtensionMatchesWiring(t *testing.T) {
	handler := newTestServer(t).Handler()

	_, env := mcpModern(t, handler, 1, "tasks/get", `{"taskId":"probe"}`)
	served := env.Error == nil || env.Error.Code != mcplib.METHOD_NOT_FOUND

	if _, advertised := serverExtensions(len(uiResources(testWidgets)) > 0)[extensionTasks]; advertised != served {
		if advertised {
			t.Fatalf("%s is advertised but tasks/get is unsupported: "+
				"pair it with mcpserver.WithTaskCapabilities", extensionTasks)
		}

		t.Fatalf("tasks/get is served but %s is not advertised: "+
			"add it to serverExtensions so hosts can discover it", extensionTasks)
	}
}

// TestUIExtensionMatchesWiring is the same contract for MCP Apps: the extension
// promises widgets, so at least one resource must declare the widget media
// type. Registering widgets without advertising the extension hides them from
// every host, which is the mirror-image mistake.
func TestUIExtensionMatchesWiring(t *testing.T) {
	handler := newTestServer(t).Handler()

	_, env := mcpModern(t, handler, 1, "resources/list", `{}`)
	if env.Error != nil {
		t.Fatalf("resources/list error: %+v", env.Error)
	}

	var result struct {
		Resources []struct {
			URI      string `json:"uri"`
			MIMEType string `json:"mimeType"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode resources/list: %v", err)
	}

	registered := false
	for _, resource := range result.Resources {
		if resource.MIMEType == uiMIMEType {
			registered = true

			break
		}
	}

	if _, advertised := serverExtensions(len(uiResources(testWidgets)) > 0)[extensionUI]; advertised != registered {
		if advertised {
			t.Fatalf("%s is advertised but no resource declares %q: "+
				"register the widget resources first", extensionUI, uiMIMEType)
		}

		t.Fatalf("widget resources are registered but %s is not advertised: "+
			"hosts will not render them", extensionUI)
	}
}

// TestDiscoverAdvertisesExtensions checks that whatever serverExtensions
// returns actually reaches the wire, so the two tests above are testing
// something a host can see.
func TestDiscoverAdvertisesExtensions(t *testing.T) {
	handler := newTestServer(t).Handler()

	_, env := mcpModern(t, handler, 1, "server/discover", `{}`)
	if env.Error != nil {
		t.Fatalf("server/discover error: %+v", env.Error)
	}

	var result struct {
		Capabilities struct {
			Extensions map[string]json.RawMessage `json:"extensions"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode server/discover: %v", err)
	}

	for name := range serverExtensions(len(uiResources(testWidgets)) > 0) {
		if _, ok := result.Capabilities.Extensions[name]; !ok {
			t.Errorf("serverExtensions declares %q but server/discover does not advertise it", name)
		}
	}

	for name := range result.Capabilities.Extensions {
		if _, ok := serverExtensions(len(uiResources(testWidgets)) > 0)[name]; !ok {
			t.Errorf("server/discover advertises %q, which serverExtensions does not declare", name)
		}
	}
}

// metaKeyRE is the _meta key grammar with its prefix made mandatory: dotted
// labels opening with a letter and closing with a letter or digit, a slash,
// then a name that opens and closes alphanumeric.
var metaKeyRE = regexp.MustCompile(
	`^[A-Za-z]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z]([A-Za-z0-9-]*[A-Za-z0-9])?)*/` +
		`[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`,
)

// newWidgetTestServer serves one widget bundle, so the UI extension is
// declared the way a build with widgets declares it.
func newWidgetTestServer(t *testing.T) *Server {
	t.Helper()

	return newResourceTestServer(t, cache.New(nil), func(o *Options) {
		o.Widgets = fstest.MapFS{"table/index.html": {Data: []byte("<!doctype html>")}}
	})
}

func TestExtensionIdentifiersFollowTheMetaKeyRules(t *testing.T) {
	spec.Satisfies(t, "mcp/sep-2133/identifiers-follow-meta-naming")

	_, env := mcpModern(t, newWidgetTestServer(t).Handler(), 1, "server/discover", `{}`)
	if env.Error != nil {
		t.Fatalf("server/discover error: %+v", env.Error)
	}

	var result struct {
		Capabilities struct {
			Extensions map[string]json.RawMessage `json:"extensions"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode server/discover: %v", err)
	}

	if _, ok := result.Capabilities.Extensions[extensionUI]; !ok {
		t.Fatalf("a build with widgets declares no %s: %s", extensionUI, env.Result)
	}

	for name := range result.Capabilities.Extensions {
		if !metaKeyRE.MatchString(name) {
			t.Errorf("extension %q is not a prefixed _meta key", name)
		}
	}
}

// The half of the fallback rule that holds: a UI-enabled tool answers a host
// that declares no UI extension in text. The Tasks half is pinned with the
// rest of that extension.
func TestAToolWithAWidgetAnswersAHostWithoutOneInText(t *testing.T) {
	spec.Satisfies(t, "mcp/sep-2133/unsupported-extension-falls-back")

	_, env := mcpModern(t, newWidgetTestServer(t).Handler(), 1, "tools/call",
		`{"name":"find","arguments":{"query":"web"}}`)
	if env.Error != nil {
		t.Fatalf("tools/call find: %+v", env.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode tools/call: %v", err)
	}

	if len(result.Content) == 0 || result.Content[0].Type != "text" ||
		result.Content[0].Text == "" {
		t.Errorf("a host without the UI extension got no text: %s", env.Result)
	}
}
