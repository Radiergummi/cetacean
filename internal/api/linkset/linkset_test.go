package linkset

import (
	"encoding/json"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// TestContextSerializationIsDeterministic pins what the catalog's ETag depends
// on: Go randomizes map iteration, so a context must still marshal to one
// sequence of bytes.
func TestContextSerializationIsDeterministic(t *testing.T) {
	ctx := Context{
		Anchor: "https://cetacean.example.com/",
		Relations: map[string][]Target{
			"service-desc": {{Href: "https://cetacean.example.com/api", Type: "application/json"}},
			"service-doc":  {{Href: "https://cetacean.example.com/api", Type: "text/html"}},
			"describedby":  {{Href: "https://cetacean.example.com/api/context.jsonld"}},
			"status":       {{Href: "https://cetacean.example.com/-/health"}},
			"item":         {{Href: "https://cetacean.example.com/mcp"}},
		},
	}

	first, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for range 100 {
		again, err := json.Marshal(ctx)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		if string(again) != string(first) {
			t.Fatalf("serialization is not stable:\n%s\n%s", first, again)
		}
	}
}

// TestContextOmitsEmptyRelations: a relation with no targets is not a
// relation, and §4.2.1 allows an absent anchor.
func TestContextOmitsEmptyRelations(t *testing.T) {
	body, err := json.Marshal(Context{
		Relations: map[string][]Target{
			"item":         {{Href: "https://cetacean.example.com/"}},
			"service-desc": {},
			"service-doc":  nil,
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	const want = `{"item":[{"href":"https://cetacean.example.com/"}]}`

	if string(body) != want {
		t.Errorf("got %s, want %s", body, want)
	}
}

// TestDocumentWrapsContexts pins the member RFC 9264 §4.2.1 names.
func TestDocumentWrapsContexts(t *testing.T) {
	body, err := json.Marshal(Document{
		Contexts: []Context{{Anchor: "https://cetacean.example.com/"}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	const want = `{"linkset":[{"anchor":"https://cetacean.example.com/"}]}`

	if string(body) != want {
		t.Errorf("got %s, want %s", body, want)
	}
}

// RFC 9264 §4.2.1 through §4.2.4.1 fix the shape of the JSON serialization:
// one member named linkset, arrays at every level even when they hold one
// thing, an href on every target, and the member names the attributes take.
func TestTheSerializationHasTheShapeRFC9264Fixes(t *testing.T) {
	spec.Satisfies(t,
		"http/rfc9264/linkset-is-the-sole-member",
		"http/rfc9264/contexts-are-wrapped-even-when-single",
		"http/rfc9264/an-anchor-may-be-present",
		"http/rfc9264/a-relation-is-a-member-of-its-context",
		"http/rfc9264/one-object-per-target",
		"http/rfc9264/targets-are-wrapped-even-when-single",
		"http/rfc9264/a-target-carries-an-href",
		"http/rfc9264/title-is-a-json-string",
		"http/rfc9264/type-is-a-string",
	)

	doc := Document{Contexts: []Context{{
		Anchor: "https://cetacean.example.com/",
		Relations: map[string][]Target{
			"service-desc": {
				{Href: "https://cetacean.example.com/api", Type: "application/json",
					Title: "OpenAPI description"},
				{Href: "https://cetacean.example.com/asyncapi"},
			},
			"status": {{Href: "https://cetacean.example.com/-/health"}},
		},
	}}}

	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(parsed) != 1 {
		t.Errorf("the document has %d members, want linkset alone: %s", len(parsed), body)
	}

	contexts, ok := parsed["linkset"].([]any)
	if !ok {
		t.Fatalf("linkset is not an array: %s", body)
	}
	if len(contexts) != 1 {
		t.Fatalf("got %d contexts, want 1", len(contexts))
	}

	context, ok := contexts[0].(map[string]any)
	if !ok {
		t.Fatalf("the context is not an object: %s", body)
	}
	if context["anchor"] != "https://cetacean.example.com/" {
		t.Errorf("anchor = %v", context["anchor"])
	}

	// Two relations, each an array — the one with a single target included.
	desc, ok := context["service-desc"].([]any)
	if !ok {
		t.Fatalf("service-desc is not an array: %s", body)
	}
	if len(desc) != 2 {
		t.Errorf("service-desc has %d targets, want 2", len(desc))
	}

	status, ok := context["status"].([]any)
	if !ok {
		t.Fatalf("status is not an array: %s", body)
	}
	if len(status) != 1 {
		t.Fatalf("status has %d targets, want 1", len(status))
	}

	first, ok := desc[0].(map[string]any)
	if !ok {
		t.Fatalf("a target is not an object: %s", body)
	}
	if first["href"] != "https://cetacean.example.com/api" {
		t.Errorf("href = %v", first["href"])
	}
	if first["type"] != "application/json" {
		t.Errorf("type = %v", first["type"])
	}
	if first["title"] != "OpenAPI description" {
		t.Errorf("title = %v", first["title"])
	}
}

// RFC 9264 §4.2.3 spells the self-target out: href stays, holding an empty
// string, rather than being left off the object.
func TestASelfTargetKeepsAnEmptyHref(t *testing.T) {
	spec.Satisfies(t, "http/rfc9264/a-self-target-carries-an-empty-href")

	body, err := json.Marshal(Target{Href: "", Title: "This document"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	href, ok := parsed["href"]
	if !ok {
		t.Fatalf("href was left off the target: %s", body)
	}
	if href != "" {
		t.Errorf("href = %v, want the empty string", href)
	}
}
