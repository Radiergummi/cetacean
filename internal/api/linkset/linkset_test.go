package linkset

import (
	"encoding/json"
	"testing"
)

// TestContextSerializationIsDeterministic pins the property the API catalog's
// ETag depends on, at the level it is produced rather than through a router:
// Go randomizes map iteration, so a context built from a map must still
// marshal to one sequence of bytes. The equivalent for JSON-LD detail
// responses is TestDetailResponse_DeterministicSerialization.
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

// TestContextOmitsEmptyRelations covers the two cases that decide whether a
// member appears at all: a relation holding no targets is not a relation, and
// an absent anchor is legal per §4.2.1 for a link about the linkset itself.
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

// TestDocumentWrapsContexts pins the one member RFC 9264 §4.2.1 names.
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
