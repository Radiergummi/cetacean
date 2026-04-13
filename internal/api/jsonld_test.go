package api

import (
	"context"
	"strings"
	"testing"

	json "github.com/goccy/go-json"
)

func TestNewDetailResponse(t *testing.T) {
	resp := NewDetailResponse(context.Background(), "/nodes/n1", "Node", map[string]any{
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
	resp := NewDetailResponse(context.Background(), "/configs/c1", "Config", map[string]any{
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
	resp := NewDetailResponse(context.Background(), "/nodes/n1", "Node", nil)

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
	resp := NewDetailResponse(context.Background(), "/nodes/n1", "Node", map[string]any{
		"zebra": 1,
		"alpha": 2,
		"node":  "value",
	})

	first, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	for i := range 100 {
		got, err := json.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(first) {
			t.Fatalf(
				"iteration %d: serialization not deterministic\nfirst: %s\n  got: %s",
				i,
				first,
				got,
			)
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
	resp := NewCollectionResponse(context.Background(), items, 10, 3, 0)

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
	resp := NewCollectionResponse(context.Background(), []int{}, 0, 50, 0)

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

type testStruct struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestItem_MarshalJSON(t *testing.T) {
	item := Item[testStruct]{
		id:  "/things/abc",
		typ: "Thing",
		val: testStruct{Name: "hello", Value: 42},
	}

	body, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}

	// Verify @id and @type are present at the top level.
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}

	if m["@id"] != "/things/abc" {
		t.Errorf("@id = %v, want /things/abc", m["@id"])
	}
	if m["@type"] != "Thing" {
		t.Errorf("@type = %v, want Thing", m["@type"])
	}
	if m["name"] != "hello" {
		t.Errorf("name = %v, want hello", m["name"])
	}
	if m["value"] != float64(42) {
		t.Errorf("value = %v, want 42", m["value"])
	}

	// @id and @type must appear first (key order).
	s := string(body)
	idPos := strings.Index(s, `"@id"`)
	typePos := strings.Index(s, `"@type"`)
	namePos := strings.Index(s, `"name"`)

	if idPos > typePos || idPos > namePos {
		t.Errorf("@id should appear before @type and name in output: %s", s)
	}

	if typePos > namePos {
		t.Errorf("@type should appear before name in output: %s", s)
	}
}

func TestItem_MarshalJSON_EmptyStruct(t *testing.T) {
	item := Item[struct{}]{
		id:  "/empty/1",
		typ: "Empty",
		val: struct{}{},
	}

	body, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}

	expected := `{"@id":"/empty/1","@type":"Empty"}`
	if string(body) != expected {
		t.Errorf("empty struct item:\n got: %s\nwant: %s", body, expected)
	}
}

func TestWrapItems(t *testing.T) {
	items := []testStruct{
		{Name: "a", Value: 1},
		{Name: "b", Value: 2},
	}

	wrapped := wrapItems(items, "Thing", func(s testStruct) string { return "/things/" + s.Name })

	if len(wrapped) != 2 {
		t.Fatalf("len = %d, want 2", len(wrapped))
	}

	for index, item := range wrapped {
		body, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatal(err)
		}
		if m["@type"] != "Thing" {
			t.Errorf("item %d: @type = %v, want Thing", index, m["@type"])
		}
		if m["@id"] != "/things/"+items[index].Name {
			t.Errorf("item %d: @id = %v, want /things/%s", index, m["@id"], items[index].Name)
		}
	}
}
