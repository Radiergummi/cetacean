package atom

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/spec"
)

func TestRender(t *testing.T) {
	updated := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	f := Feed{
		Title:   "Cetacean — Services",
		Author:  &Author{Name: "Cetacean"},
		ID:      "tag:example.com,2026:/services",
		Updated: updated,
		Links: []Link{
			{Rel: "self", Href: "/services.atom"},
			{Rel: "alternate", Href: "/services", Type: "application/json"},
		},
		Entries: []Entry{
			{
				ID:      "urn:cetacean:history:42",
				Title:   "update myservice",
				Updated: updated,
				Content: ContentElement{Type: "text", Value: "Scaled to 3 replicas"},
				Links: []Link{
					{Rel: "alternate", Href: "/services/abc123"},
				},
				Categories: []Category{
					{Term: "service"},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := Render(&buf, f); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()

	if !strings.Contains(out, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Error("missing XML declaration")
	}
	if !strings.Contains(out, `xmlns="http://www.w3.org/2005/Atom"`) {
		t.Error("missing Atom namespace")
	}
	if !strings.Contains(out, `<title>Cetacean — Services</title>`) {
		t.Error("missing feed title")
	}
	if !strings.Contains(out, `<author>`) || !strings.Contains(out, `<name>Cetacean</name>`) {
		t.Error("missing feed author")
	}
	if !strings.Contains(out, `tag:example.com,2026:/services`) {
		t.Error("missing feed id")
	}
	if !strings.Contains(out, `urn:cetacean:history:42`) {
		t.Error("missing entry id")
	}
	if !strings.Contains(out, `<content type="text">Scaled to 3 replicas</content>`) {
		t.Error("missing entry content with type attribute")
	}
	if !strings.Contains(out, `<category term="service"`) {
		t.Error("missing category")
	}
}

func TestRenderEmptyFeed(t *testing.T) {
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	f := Feed{
		Title:   "Cetacean — Nodes",
		ID:      "tag:example.com,2026:/nodes",
		Updated: now,
	}

	var buf bytes.Buffer
	if err := Render(&buf, f); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `<title>Cetacean — Nodes</title>`) {
		t.Error("missing feed title")
	}
	if !strings.Contains(out, `2026-04-01T00:00:00Z`) {
		t.Error("missing updated timestamp")
	}
}

func TestRenderPaginationLinks(t *testing.T) {
	f := Feed{
		Title:   "Test",
		ID:      "tag:example.com,2026:/test",
		Updated: time.Now(),
		Links: []Link{
			{Rel: "self", Href: "/test.atom?limit=50"},
			{Rel: "next", Href: "/test.atom?before=100&limit=50"},
			{Rel: "previous", Href: "/test.atom?before=200&limit=50"},
		},
	}

	var buf bytes.Buffer
	if err := Render(&buf, f); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `rel="next"`) {
		t.Error("missing next link")
	}
	if !strings.Contains(out, `rel="previous"`) {
		t.Error("missing previous link")
	}
	if !strings.Contains(out, `before=100`) {
		t.Error("next link missing cursor")
	}
}

func TestRenderAuthorOmitted(t *testing.T) {
	f := Feed{
		Title:   "No Author",
		ID:      "tag:example.com,2026:/test",
		Updated: time.Now(),
	}

	var buf bytes.Buffer
	if err := Render(&buf, f); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, `<author>`) {
		t.Error("author element should be omitted when nil")
	}
}

// TestTheSerializationHasTheShapeRFC4287Fixes parses what Render wrote rather
// than matching substrings in it: the rules are about which elements exist and
// how many, and a document that does not parse has none of them.
func TestTheSerializationHasTheShapeRFC4287Fixes(t *testing.T) {
	spec.Satisfies(t,
		"http/rfc4287/documents-are-well-formed-xml",
		"http/rfc4287/a-text-construct-may-carry-a-type",
		"http/rfc4287/a-person-has-exactly-one-name",
		"http/rfc4287/a-feed-has-exactly-one-id",
		"http/rfc4287/a-feed-has-exactly-one-title",
		"http/rfc4287/a-feed-has-exactly-one-updated",
		"http/rfc4287/an-entry-has-exactly-one-id",
		"http/rfc4287/an-entry-has-exactly-one-title",
		"http/rfc4287/an-entry-has-exactly-one-updated",
		"http/rfc4287/an-entry-may-carry-categories",
		"http/rfc4287/a-category-has-a-term",
		"http/rfc4287/a-link-has-an-href",
		"http/rfc4287/a-link-may-carry-a-rel",
		"http/rfc4287/content-type-may-be-html",
		"http/rfc4287/updated-may-change-over-time",
	)

	updated := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	var buf bytes.Buffer
	if err := Render(&buf, Feed{
		Title:   "Cetacean — Services",
		Author:  &Author{Name: "Cetacean"},
		ID:      "tag:example.com,2026:/services",
		Updated: updated,
		Links:   []Link{{Rel: "self", Href: "/services.atom", Type: "application/atom+xml"}},
		Entries: []Entry{{
			ID:         "urn:cetacean:history:42",
			Title:      "update myservice",
			Updated:    updated,
			Content:    ContentElement{Type: "html", Value: "Scaled to 3 replicas"},
			Links:      []Link{{Rel: "alternate", Href: "/services/abc123"}},
			Categories: []Category{{Term: "service"}},
		}},
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	var parsed struct {
		Title   []string `xml:"title"`
		ID      []string `xml:"id"`
		Updated []string `xml:"updated"`
		Author  []struct {
			Name []string `xml:"name"`
		} `xml:"author"`
		Links []struct {
			Rel  string `xml:"rel,attr"`
			Href string `xml:"href,attr"`
			Type string `xml:"type,attr"`
		} `xml:"link"`
		Entries []struct {
			ID      []string `xml:"id"`
			Title   []string `xml:"title"`
			Updated []string `xml:"updated"`
			Content []struct {
				Type  string `xml:"type,attr"`
				Value string `xml:",chardata"`
			} `xml:"content"`
			Categories []struct {
				Term string `xml:"term,attr"`
			} `xml:"category"`
		} `xml:"entry"`
	}

	if err := xml.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("the rendered feed is not well-formed XML: %v\n%s", err, buf.String())
	}

	exactlyOne := func(what string, got int) {
		t.Helper()
		if got != 1 {
			t.Errorf("%s appears %d times, want exactly one", what, got)
		}
	}

	exactlyOne("atom:feed/atom:id", len(parsed.ID))
	exactlyOne("atom:feed/atom:title", len(parsed.Title))
	exactlyOne("atom:feed/atom:updated", len(parsed.Updated))
	exactlyOne("atom:feed/atom:author", len(parsed.Author))
	if len(parsed.Author) == 1 {
		exactlyOne("atom:author/atom:name", len(parsed.Author[0].Name))
	}

	// The feed title carries no type attribute, so it is a text construct by
	// §3.1.1's default — which is what the titles here are.
	if strings.Contains(buf.String(), `<title type=`) {
		t.Error("the feed title carries a type attribute")
	}

	if len(parsed.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(parsed.Entries))
	}

	entry := parsed.Entries[0]
	exactlyOne("atom:entry/atom:id", len(entry.ID))
	exactlyOne("atom:entry/atom:title", len(entry.Title))
	exactlyOne("atom:entry/atom:updated", len(entry.Updated))
	exactlyOne("atom:entry/atom:content", len(entry.Content))

	if len(entry.Content) == 1 && entry.Content[0].Type != "html" {
		t.Errorf("content type = %q, want html", entry.Content[0].Type)
	}
	if len(entry.Categories) != 1 || entry.Categories[0].Term != "service" {
		t.Errorf("categories = %+v, want one with term=service", entry.Categories)
	}

	if len(parsed.Links) != 1 {
		t.Fatalf("got %d feed links, want 1", len(parsed.Links))
	}
	if parsed.Links[0].Href != "/services.atom" {
		t.Errorf("href = %q", parsed.Links[0].Href)
	}
	if parsed.Links[0].Rel != "self" {
		t.Errorf("rel = %q, want self", parsed.Links[0].Rel)
	}

	// The updated values are whatever the caller handed in, so a publisher
	// that moves one moves it.
	if parsed.Updated[0] != updated.Format(time.RFC3339) {
		t.Errorf("updated = %q, want %q", parsed.Updated[0], updated.Format(time.RFC3339))
	}
}

// RFC 4287 §4.1.3.3 keeps markup out of html-typed content by escaping it, so
// a feed carrying an HTML fragment is still one XML document.
func TestHTMLContentIsEscaped(t *testing.T) {
	spec.Satisfies(t,
		"http/rfc4287/html-content-has-no-child-elements",
		"http/rfc4287/html-markup-is-escaped",
		"http/rfc4287/html-content-suits-a-div",
	)

	var buf bytes.Buffer
	if err := Render(&buf, Feed{
		Title:   "Cetacean",
		Author:  &Author{Name: "Cetacean"},
		ID:      "tag:example.com,2026:/history",
		Updated: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		Entries: []Entry{{
			ID:      "urn:cetacean:history:1",
			Title:   "an event",
			Updated: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
			Content: ContentElement{
				Type:  "html",
				Value: `<p>Scaled <strong>api</strong> to 3</p>`,
			},
		}},
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "&lt;p&gt;") {
		t.Errorf("the HTML markup was not escaped:\n%s", out)
	}
	if strings.Contains(out, "<p>") {
		t.Errorf("the content carries a child element:\n%s", out)
	}

	// Reading it back gives the fragment again, which is what a reader would
	// drop into a DIV.
	var parsed struct {
		Entries []struct {
			Content string `xml:"content"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := parsed.Entries[0].Content; got != `<p>Scaled <strong>api</strong> to 3</p>` {
		t.Errorf("content round-tripped as %q", got)
	}
}
