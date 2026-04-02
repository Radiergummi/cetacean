package atom

import (
	"bytes"
	"strings"
	"testing"
	"time"
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
