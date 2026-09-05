package mcp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// contentItem is one entry of a tool result's content array, decoded loosely
// enough to tell a text item from a resource link. The tests below go through
// the real transport rather than calling the handler, because the links are
// attached where the CallToolResult is assembled — a handler returns only its
// JSON text, so none of this exists until mcp-go has serialized the result.
type contentItem struct {
	Type     string `json:"type"`
	URI      string `json:"uri"`
	Name     string `json:"name"`
	MIMEType string `json:"mimeType"`
	Text     string `json:"text"`
}

type toolCallResult struct {
	Content           []contentItem   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

func callTool(t *testing.T, handler http.Handler, params string) toolCallResult {
	t.Helper()

	_, envelope := mcpModern(t, handler, 1, "tools/call", params)
	if envelope.Error != nil {
		t.Fatalf("tools/call failed: %+v", envelope.Error)
	}

	var result toolCallResult
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode tool result: %v (raw %s)", err, envelope.Result)
	}

	if result.IsError {
		t.Fatalf("tool reported an error: %s", envelope.Result)
	}

	return result
}

// resourceLinks keeps only the resource_link items, which is the assertion
// every test here makes: the text item must still come first, and anything
// else in the array is not what is being tested.
func resourceLinks(items []contentItem) []contentItem {
	var links []contentItem

	for _, item := range items {
		if item.Type == "resource_link" {
			links = append(links, item)
		}
	}

	return links
}

func linkURIs(items []contentItem) []string {
	uris := make([]string, 0, len(items))

	for _, item := range resourceLinks(items) {
		uris = append(uris, item.URI)
	}

	return uris
}

// A listing's rows carry ids; the links carry the URI those ids resolve at, so
// a host can offer somewhere to go and a client can resources/read one without
// the model first working out how to spell a cetacean:// path.
func TestFindOffersAResourceLinkPerRow(t *testing.T) {
	c := seededDescribeCache()
	handler := newResourceTestServer(t, c).Handler()

	result := callTool(t, handler, `{"name":"find","arguments":{"type":"services"}}`)

	if len(result.Content) == 0 || result.Content[0].Type != "text" {
		t.Fatalf("content must lead with the answer, got %+v", result.Content)
	}

	links := resourceLinks(result.Content)
	if len(links) == 0 {
		t.Fatal("no resource links on a services listing")
	}

	for _, link := range links {
		if link.MIMEType != mcpMIMEType {
			t.Errorf("link %s has mimeType %q, want %q", link.URI, link.MIMEType, mcpMIMEType)
		}

		if link.Name == "" {
			t.Errorf("link %s has no name; the field is required", link.URI)
		}
	}

	if got := linkURIs(result.Content); got[0] != "cetacean://services/svc1" {
		t.Errorf("first link = %q, want cetacean://services/svc1", got[0])
	}
}

// The links must describe the page that was actually returned. A link to a
// record the caller was not shown — filtered out, or on another page — is a
// pointer to something the result does not mention.
func TestFindLinksFollowTheFilteredPage(t *testing.T) {
	c := seededDescribeCache()
	c.SetService(swarm.Service{
		ID:   "svc-other",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "unrelated"}},
	})

	handler := newResourceTestServer(t, c).Handler()

	result := callTool(t, handler,
		`{"name":"find","arguments":{"type":"services","query":"unrelated"}}`)

	uris := linkURIs(result.Content)
	if len(uris) != 1 || uris[0] != "cetacean://services/svc-other" {
		t.Errorf("links = %v, want only the matching service", uris)
	}
}

// describe's whole point is traversal: the digest names a resource's
// cross-references, and the links are what make them reachable.
func TestDescribeLinksTheResourceAndItsCrossReferences(t *testing.T) {
	c := seededDescribeCache()
	handler := newResourceTestServer(t, c).Handler()

	result := callTool(t, handler,
		`{"name":"describe","arguments":{"type":"service","id":"svc1"}}`)

	uris := linkURIs(result.Content)
	if len(uris) == 0 {
		t.Fatal("describe returned no resource links")
	}

	if uris[0] != "cetacean://services/svc1" {
		t.Errorf("first link = %q, want the described resource itself", uris[0])
	}

	if len(uris) < 2 {
		t.Errorf("links = %v, want the cross-referenced resources too", uris)
	}
}

// raw changes the shape of the answer, never the scope — the same rule the
// post-filters follow. Losing the way onward would be a scope change.
func TestFindRawStillOffersLinks(t *testing.T) {
	c := seededDescribeCache()
	handler := newResourceTestServer(t, c).Handler()

	result := callTool(t, handler,
		`{"name":"find","arguments":{"type":"services","raw":true}}`)

	if len(result.StructuredContent) == 0 {
		t.Error("raw result carried no structuredContent, though find declares an output schema")
	}

	if got := linkURIs(result.Content); len(got) == 0 {
		t.Error("raw listing offered no resource links")
	}
}

// A page nobody is traversing one row at a time should not arrive as a wall of
// links: find defaults to 200 records for the table widget, and a host renders
// every one of these as something to open.
func TestResourceLinksAreCapped(t *testing.T) {
	c := seededDescribeCache()

	for i := range maxResourceLinks * 2 {
		c.SetService(swarm.Service{
			ID: "bulk-" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
				Name: "bulk-" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			}},
		})
	}

	handler := newResourceTestServer(t, c).Handler()

	result := callTool(t, handler, `{"name":"find","arguments":{"type":"services"}}`)

	if got := len(resourceLinks(result.Content)); got != maxResourceLinks {
		t.Errorf("links = %d, want the cap of %d", got, maxResourceLinks)
	}
}
