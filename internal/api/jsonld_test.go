package api

import (
	"testing"

	json "github.com/goccy/go-json"
)

func TestNewDetailResponse(t *testing.T) {
	resp := NewDetailResponse("/nodes/n1", "Node", map[string]any{
		"node": "test-value",
	})

	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}

	if m["@context"] != jsonLDContext {
		t.Errorf("@context = %v, want %s", m["@context"], jsonLDContext)
	}
	if m["@id"] != "/nodes/n1" {
		t.Errorf("@id = %v, want /nodes/n1", m["@id"])
	}
	if m["@type"] != "Node" {
		t.Errorf("@type = %v, want Node", m["@type"])
	}
	if m["node"] != "test-value" {
		t.Errorf("node = %v, want test-value", m["node"])
	}
}

func TestNewDetailResponse_MultipleExtras(t *testing.T) {
	resp := NewDetailResponse("/configs/c1", "Config", map[string]any{
		"config":   "cfg-data",
		"services": []string{"svc1"},
	})

	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}

	if m["@context"] != jsonLDContext {
		t.Errorf("@context = %v, want %s", m["@context"], jsonLDContext)
	}
	if m["@id"] != "/configs/c1" {
		t.Errorf("@id = %v, want /configs/c1", m["@id"])
	}
	if m["@type"] != "Config" {
		t.Errorf("@type = %v, want Config", m["@type"])
	}
	if m["config"] != "cfg-data" {
		t.Errorf("config = %v, want cfg-data", m["config"])
	}
}

func TestNewDetailResponse_NilExtras(t *testing.T) {
	resp := NewDetailResponse("/nodes/n1", "Node", nil)

	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}

	if len(m) != 3 {
		t.Errorf("expected 3 keys, got %d", len(m))
	}
	if m["@context"] != jsonLDContext {
		t.Errorf("@context = %v, want %s", m["@context"], jsonLDContext)
	}
}

func TestDetailResponse_DeterministicSerialization(t *testing.T) {
	// Marshal the same response multiple times and verify identical output.
	resp := NewDetailResponse("/nodes/n1", "Node", map[string]any{
		"zebra": 1,
		"alpha": 2,
		"node":  "value",
	})

	first, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		got, err := json.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(first) {
			t.Fatalf("iteration %d: serialization not deterministic\nfirst: %s\n  got: %s", i, first, got)
		}
	}

	// Verify key order: @context, @id, @type, then extras alphabetically.
	expected := `{"@context":"/api/context.jsonld","@id":"/nodes/n1","@type":"Node","alpha":2,"node":"value","zebra":1}`
	if string(first) != expected {
		t.Errorf("unexpected key order:\n got: %s\nwant: %s", first, expected)
	}
}

func TestNewCollectionResponse(t *testing.T) {
	items := []string{"a", "b", "c"}
	resp := NewCollectionResponse(items, 10, 3, 0)

	if resp.Context != jsonLDContext {
		t.Errorf("Context = %s, want %s", resp.Context, jsonLDContext)
	}
	if resp.Type != "Collection" {
		t.Errorf("Type = %s, want Collection", resp.Type)
	}
	if len(resp.Items) != 3 {
		t.Errorf("Items length = %d, want 3", len(resp.Items))
	}
	if resp.Total != 10 {
		t.Errorf("Total = %d, want 10", resp.Total)
	}
	if resp.Limit != 3 {
		t.Errorf("Limit = %d, want 3", resp.Limit)
	}
	if resp.Offset != 0 {
		t.Errorf("Offset = %d, want 0", resp.Offset)
	}
}

func TestNewCollectionResponse_Empty(t *testing.T) {
	resp := NewCollectionResponse([]int{}, 0, 50, 0)

	if resp.Context != jsonLDContext {
		t.Errorf("Context = %s, want %s", resp.Context, jsonLDContext)
	}
	if len(resp.Items) != 0 {
		t.Errorf("Items length = %d, want 0", len(resp.Items))
	}
	if resp.Total != 0 {
		t.Errorf("Total = %d, want 0", resp.Total)
	}
}
