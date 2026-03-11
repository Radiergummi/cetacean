package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePagination_Defaults(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	p := parsePagination(r)

	if p.Limit != 50 {
		t.Errorf("expected limit 50, got %d", p.Limit)
	}
	if p.Offset != 0 {
		t.Errorf("expected offset 0, got %d", p.Offset)
	}
	if p.Dir != "asc" {
		t.Errorf("expected dir asc, got %s", p.Dir)
	}
	if p.Sort != "" {
		t.Errorf("expected empty sort, got %s", p.Sort)
	}
}

func TestParsePagination_Custom(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?limit=10&offset=20&sort=name&dir=desc", nil)
	p := parsePagination(r)

	if p.Limit != 10 {
		t.Errorf("expected limit 10, got %d", p.Limit)
	}
	if p.Offset != 20 {
		t.Errorf("expected offset 20, got %d", p.Offset)
	}
	if p.Sort != "name" {
		t.Errorf("expected sort name, got %s", p.Sort)
	}
	if p.Dir != "desc" {
		t.Errorf("expected dir desc, got %s", p.Dir)
	}
}

func TestParsePagination_MaxLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?limit=9999", nil)
	p := parsePagination(r)

	if p.Limit != 200 {
		t.Errorf("expected limit clamped to 200, got %d", p.Limit)
	}
}

func TestApplyPagination(t *testing.T) {
	items := make([]int, 10)
	for i := range items {
		items[i] = i
	}

	p := PageParams{Limit: 3, Offset: 2}
	result := applyPagination(items, p)

	if result.Total != 10 {
		t.Errorf("expected total 10, got %d", result.Total)
	}
	if len(result.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.Items))
	}
	if result.Items[0] != 2 || result.Items[1] != 3 || result.Items[2] != 4 {
		t.Errorf("expected items [2,3,4], got %v", result.Items)
	}
	if result.Context != jsonLDContext {
		t.Errorf("expected @context %s, got %s", jsonLDContext, result.Context)
	}
	if result.Type != "Collection" {
		t.Errorf("expected @type Collection, got %s", result.Type)
	}
	if result.Limit != 3 {
		t.Errorf("expected limit 3, got %d", result.Limit)
	}
	if result.Offset != 2 {
		t.Errorf("expected offset 2, got %d", result.Offset)
	}
}

func TestApplyPagination_BeyondEnd(t *testing.T) {
	items := []int{1, 2, 3}

	p := PageParams{Limit: 10, Offset: 100}
	result := applyPagination(items, p)

	if result.Total != 3 {
		t.Errorf("expected total 3, got %d", result.Total)
	}
	if result.Items == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(result.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(result.Items))
	}
}

type testItem struct {
	Name string
}

func TestSortItems(t *testing.T) {
	items := []testItem{{Name: "Charlie"}, {Name: "Alice"}, {Name: "Bob"}}
	accessors := map[string]func(testItem) string{
		"name": func(i testItem) string { return i.Name },
	}

	sorted := sortItems(items, "name", "asc", accessors)

	if sorted[0].Name != "Alice" || sorted[1].Name != "Bob" || sorted[2].Name != "Charlie" {
		t.Errorf("expected [Alice, Bob, Charlie], got [%s, %s, %s]", sorted[0].Name, sorted[1].Name, sorted[2].Name)
	}
	// Original should be unchanged
	if items[0].Name != "Charlie" {
		t.Errorf("original slice was modified")
	}
}

func TestSortItems_Desc(t *testing.T) {
	items := []testItem{{Name: "Alice"}, {Name: "Charlie"}, {Name: "Bob"}}
	accessors := map[string]func(testItem) string{
		"name": func(i testItem) string { return i.Name },
	}

	sorted := sortItems(items, "name", "desc", accessors)

	if sorted[0].Name != "Charlie" || sorted[1].Name != "Bob" || sorted[2].Name != "Alice" {
		t.Errorf("expected [Charlie, Bob, Alice], got [%s, %s, %s]", sorted[0].Name, sorted[1].Name, sorted[2].Name)
	}
}

func TestSortItems_InvalidKey(t *testing.T) {
	items := []testItem{{Name: "Charlie"}, {Name: "Alice"}, {Name: "Bob"}}
	accessors := map[string]func(testItem) string{
		"name": func(i testItem) string { return i.Name },
	}

	sorted := sortItems(items, "unknown", "asc", accessors)

	if sorted[0].Name != "Charlie" || sorted[1].Name != "Alice" || sorted[2].Name != "Bob" {
		t.Errorf("expected original order preserved, got [%s, %s, %s]", sorted[0].Name, sorted[1].Name, sorted[2].Name)
	}
}
