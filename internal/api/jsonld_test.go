package api

import (
	"testing"
)

func TestNewDetailResponse(t *testing.T) {
	resp := NewDetailResponse("/nodes/n1", "Node", map[string]any{
		"node": "test-value",
	})

	if resp["@context"] != jsonLDContext {
		t.Errorf("@context = %v, want %s", resp["@context"], jsonLDContext)
	}
	if resp["@id"] != "/nodes/n1" {
		t.Errorf("@id = %v, want /nodes/n1", resp["@id"])
	}
	if resp["@type"] != "Node" {
		t.Errorf("@type = %v, want Node", resp["@type"])
	}
	if resp["node"] != "test-value" {
		t.Errorf("node = %v, want test-value", resp["node"])
	}
}

func TestNewDetailResponse_MultipleExtras(t *testing.T) {
	resp := NewDetailResponse("/configs/c1", "Config", map[string]any{
		"config":   "cfg-data",
		"services": []string{"svc1"},
	})

	if resp["@context"] != jsonLDContext {
		t.Errorf("@context = %v, want %s", resp["@context"], jsonLDContext)
	}
	if resp["@id"] != "/configs/c1" {
		t.Errorf("@id = %v, want /configs/c1", resp["@id"])
	}
	if resp["@type"] != "Config" {
		t.Errorf("@type = %v, want Config", resp["@type"])
	}
	if resp["config"] != "cfg-data" {
		t.Errorf("config = %v, want cfg-data", resp["config"])
	}
	svcs, ok := resp["services"].([]string)
	if !ok || len(svcs) != 1 || svcs[0] != "svc1" {
		t.Errorf("services = %v, want [svc1]", resp["services"])
	}
}

func TestNewDetailResponse_NilExtras(t *testing.T) {
	resp := NewDetailResponse("/nodes/n1", "Node", nil)

	if len(resp) != 3 {
		t.Errorf("expected 3 keys, got %d", len(resp))
	}
	if resp["@context"] != jsonLDContext {
		t.Errorf("@context = %v, want %s", resp["@context"], jsonLDContext)
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
