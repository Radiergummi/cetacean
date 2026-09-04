package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// promptListing is the prompts/list result.
type promptListing struct {
	Prompts []struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Arguments   []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
		} `json:"arguments"`
	} `json:"prompts"`
}

// listPrompts drives prompts/list through the real transport.
func listPrompts(t *testing.T, handler http.Handler) promptListing {
	t.Helper()

	_, envelope := mcpModern(t, handler, 1, "prompts/list", `{}`)
	if envelope.Error != nil {
		t.Fatalf("prompts/list failed: %+v", envelope.Error)
	}

	var listing promptListing
	if err := json.Unmarshal(envelope.Result, &listing); err != nil {
		t.Fatalf("decode prompts/list: %v (raw %s)", err, envelope.Result)
	}

	return listing
}

// promptNames returns the listed prompt names, for asserting by name rather
// than by count — a prompt moving tier should fail visibly.
func promptNames(listing promptListing) []string {
	names := make([]string, 0, len(listing.Prompts))
	for _, prompt := range listing.Prompts {
		names = append(names, prompt.Name)
	}

	return names
}

// TestPromptsAreRegisteredAndReachable guards registration and end-to-end
// reachability. A unit test on promptCatalog() passes happily whether or not
// registerPrompts() ever ran on a real server; this drives the real transport
// so a missing registerPrompts() call, an empty catalog, or a broken handler
// fails here instead of silently serving no prompts.
func TestPromptsAreRegisteredAndReachable(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	listing := listPrompts(t, handler)
	if len(listing.Prompts) == 0 {
		t.Fatal("prompts/list returned nothing; prompts are not registered")
	}

	_, envelope := mcpModern(t, handler, 2, "prompts/get",
		`{"name":"review_capacity","arguments":{}}`)
	if envelope.Error != nil {
		t.Fatalf("prompts/get failed: %+v", envelope.Error)
	}
}

// TestPromptsListIsTierGated — at tier 0 only the diagnostic prompts exist.
// Asserted by name so a prompt changing tier fails here rather than silently
// appearing in a read-only deployment.
func TestPromptsListIsTierGated(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	got := promptNames(listPrompts(t, handler))
	want := map[string]bool{
		"diagnose_service":      true,
		"explain_unschedulable": true,
		"review_capacity":       true,
	}

	if len(got) != len(want) {
		t.Fatalf("tier 0 listed %v, want exactly %d diagnostic prompts", got, len(want))
	}

	for _, name := range got {
		if !want[name] {
			t.Errorf("tier 0 listed %q, which is not a tier-0 prompt", name)
		}
	}
}

// TestPromptsGetExpandsThroughTheTransport confirms the handler is reachable
// and its argument survives JSON round-tripping.
func TestPromptsGetExpandsThroughTheTransport(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	_, envelope := mcpModern(t, handler, 1, "prompts/get",
		`{"name":"diagnose_service","arguments":{"service":"web"}}`)
	if envelope.Error != nil {
		t.Fatalf("prompts/get failed: %+v", envelope.Error)
	}

	var result struct {
		Messages []struct {
			Role    string `json:"role"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode prompts/get: %v (raw %s)", err, envelope.Result)
	}

	if len(result.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(result.Messages))
	}

	if result.Messages[0].Role != "user" {
		t.Errorf("role = %q, want user", result.Messages[0].Role)
	}

	if want := "web"; !strings.Contains(result.Messages[0].Content.Text, want) {
		t.Errorf("expanded text does not name %q", want)
	}
}

// TestPromptsGetRejectsAMissingArgument — the handler's guard has to survive
// the transport, since mcp-go does not enforce Required itself.
func TestPromptsGetRejectsAMissingArgument(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	_, envelope := mcpModern(t, handler, 1, "prompts/get",
		`{"name":"diagnose_service","arguments":{}}`)
	if envelope.Error == nil {
		t.Fatal("prompts/get with no argument succeeded; the service name is required")
	}
}

// newPromptTestServer wires a server at the given operations level.
func newPromptTestServer(t *testing.T, opsLevel config.OperationsLevel) *Server {
	t.Helper()

	return newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, opsLevel)
}
