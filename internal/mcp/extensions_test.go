package mcp

import (
	"encoding/json"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// TestTasksExtensionMatchesWiring holds the advertisement and the capability
// together: if server/discover names the Tasks extension, tasks/get must work.
//
// The check is behavioural on purpose. mcp-go gates every tasks/* method on
// WithTaskCapabilities, which is a separate option from WithExtensions, so the
// two can drift silently — a host would read the extension, call tasks/get, and
// be told the method does not exist. This test starts passing for the right
// reason when the Tasks phase wires both, and fails if it wires only one.
func TestTasksExtensionMatchesWiring(t *testing.T) {
	handler := newTestServer(t).Handler()

	_, env := mcpModern(t, handler, 1, "tasks/get", `{"taskId":"probe"}`)
	served := env.Error == nil || env.Error.Code != mcplib.METHOD_NOT_FOUND

	if _, advertised := serverExtensions()[extensionTasks]; advertised != served {
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

	if _, advertised := serverExtensions()[extensionUI]; advertised != registered {
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

	for name := range serverExtensions() {
		if _, ok := result.Capabilities.Extensions[name]; !ok {
			t.Errorf("serverExtensions declares %q but server/discover does not advertise it", name)
		}
	}

	for name := range result.Capabilities.Extensions {
		if _, ok := serverExtensions()[name]; !ok {
			t.Errorf("server/discover advertises %q, which serverExtensions does not declare", name)
		}
	}
}
