package cache

import (
	"fmt"
	"strings"
	"testing"
)

func TestHistory_Append(t *testing.T) {
	h := NewHistory(10)
	h.Append(HistoryEntry{Type: "service", Action: "update", ResourceID: "s1", Name: "web"})

	entries := h.List(HistoryQuery{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != 1 {
		t.Errorf("expected ID 1, got %d", entries[0].ID)
	}
	if entries[0].Name != "web" {
		t.Errorf("expected name 'web', got %q", entries[0].Name)
	}
	if entries[0].Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
}

func TestHistory_RingOverflow(t *testing.T) {
	h := NewHistory(3)
	for i := range 4 {
		h.Append(HistoryEntry{Type: "service", Action: "update", Name: names[i]})
	}

	entries := h.List(HistoryQuery{})
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// Newest first
	if entries[0].Name != "d" {
		t.Errorf("expected newest entry 'd', got %q", entries[0].Name)
	}
	if entries[1].Name != "c" {
		t.Errorf("expected second entry 'c', got %q", entries[1].Name)
	}
	if entries[2].Name != "b" {
		t.Errorf("expected oldest entry 'b', got %q", entries[2].Name)
	}
}

var names = []string{"a", "b", "c", "d", "e"}

func TestHistory_FilterByType(t *testing.T) {
	h := NewHistory(10)
	h.Append(HistoryEntry{Type: "service", Name: "s1"})
	h.Append(HistoryEntry{Type: "node", Name: "n1"})
	h.Append(HistoryEntry{Type: "service", Name: "s2"})

	entries := h.List(HistoryQuery{Type: "service"})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Type != "service" {
			t.Errorf("expected type 'service', got %q", e.Type)
		}
	}
}

func TestHistory_FilterByResourceID(t *testing.T) {
	h := NewHistory(10)
	h.Append(HistoryEntry{Type: "service", ResourceID: "s1", Name: "a"})
	h.Append(HistoryEntry{Type: "service", ResourceID: "s2", Name: "b"})
	h.Append(HistoryEntry{Type: "service", ResourceID: "s1", Name: "c"})

	entries := h.List(HistoryQuery{ResourceID: "s1"})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.ResourceID != "s1" {
			t.Errorf("expected resourceID 's1', got %q", e.ResourceID)
		}
	}
}

func TestHistory_Limit(t *testing.T) {
	h := NewHistory(100)
	for range 50 {
		h.Append(HistoryEntry{Type: "service", Name: "x"})
	}

	entries := h.List(HistoryQuery{Limit: 5})
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
}

func TestHistory_Count_Empty(t *testing.T) {
	h := NewHistory(10)

	if c := h.Count(); c != 0 {
		t.Fatalf("expected 0, got %d", c)
	}
}

func TestHistory_Count_AfterAppends(t *testing.T) {
	h := NewHistory(10)

	for range 5 {
		h.Append(HistoryEntry{Type: "service", Action: "update"})
	}

	if c := h.Count(); c != 5 {
		t.Fatalf("expected 5, got %d", c)
	}
}

func TestHistory_Since_Basic(t *testing.T) {
	h := NewHistory(10)

	for i := range 5 {
		h.Append(HistoryEntry{Type: "service", Action: "update", Name: names[i]})
	}

	entries, ok := h.Since(2)
	if !ok {
		t.Fatal("expected ok=true")
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries after ID 2, got %d", len(entries))
	}

	// Chronological order (oldest first)
	if entries[0].Name != "c" || entries[1].Name != "d" || entries[2].Name != "e" {
		t.Errorf("unexpected order: %v", entries)
	}
}

func TestHistory_Since_CaughtUp(t *testing.T) {
	h := NewHistory(10)

	for range 3 {
		h.Append(HistoryEntry{Type: "service"})
	}

	entries, ok := h.Since(3)
	if !ok {
		t.Fatal("expected ok=true")
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries when caught up, got %d", len(entries))
	}
}

func TestHistory_Since_Overwritten(t *testing.T) {
	h := NewHistory(3) // ring size 3

	for i := range 5 {
		h.Append(HistoryEntry{Type: "service", Name: names[i]})
	}

	// ID 1 and 2 have been overwritten (ring holds IDs 3, 4, 5)
	_, ok := h.Since(1)
	if ok {
		t.Fatal("expected ok=false for overwritten ID")
	}
}

func TestHistory_Since_FutureID(t *testing.T) {
	h := NewHistory(10)

	h.Append(HistoryEntry{Type: "service"})

	_, ok := h.Since(999)
	if ok {
		t.Fatal("expected ok=false for future ID")
	}
}

func TestHistory_Since_Zero(t *testing.T) {
	h := NewHistory(10)

	for i := range 3 {
		h.Append(HistoryEntry{Type: "service", Name: names[i]})
	}

	entries, ok := h.Since(0)
	if !ok {
		t.Fatal("expected ok=true")
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries after ID 0, got %d", len(entries))
	}

	if entries[0].Name != "a" {
		t.Errorf("expected oldest first, got %q", entries[0].Name)
	}
}

func TestHistory_Since_WrappedRing(t *testing.T) {
	h := NewHistory(3)

	for i := range 5 {
		h.Append(HistoryEntry{Type: "service", Name: names[i]})
	}

	// Ring holds IDs 3 ("c"), 4 ("d"), 5 ("e")
	entries, ok := h.Since(3)
	if !ok {
		t.Fatal("expected ok=true")
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after ID 3, got %d", len(entries))
	}

	if entries[0].Name != "d" || entries[1].Name != "e" {
		t.Errorf("unexpected entries: %v", entries)
	}
}

func TestHistoryListBeforeID(t *testing.T) {
	h := NewHistory(100)

	for i := range 10 {
		h.Append(HistoryEntry{
			Type:       EventService,
			Action:     "update",
			ResourceID: "svc1",
			Name:       fmt.Sprintf("entry-%d", i),
		})
	}

	all := h.List(HistoryQuery{Limit: 10})
	if len(all) != 10 {
		t.Fatalf("got %d entries, want 10", len(all))
	}

	cursor := all[2].ID // 3rd newest
	result := h.List(HistoryQuery{BeforeID: cursor, Limit: 5})

	if len(result) != 5 {
		t.Fatalf("got %d entries, want 5", len(result))
	}

	if result[0].ID != all[3].ID {
		t.Errorf("first result ID = %d, want %d", result[0].ID, all[3].ID)
	}
}

func TestHistoryListBeforeIDWithTypeFilter(t *testing.T) {
	h := NewHistory(100)

	for i := range 10 {
		typ := EventService
		if i%2 == 0 {
			typ = EventNode
		}
		h.Append(HistoryEntry{
			Type:       typ,
			Action:     "update",
			ResourceID: fmt.Sprintf("r%d", i),
			Name:       fmt.Sprintf("entry-%d", i),
		})
	}

	all := h.List(HistoryQuery{Limit: 10})
	cursor := all[1].ID

	result := h.List(HistoryQuery{BeforeID: cursor, Type: EventService, Limit: 10})

	for _, e := range result {
		if e.Type != EventService {
			t.Errorf("got type %q, want service", e.Type)
		}
		if e.ID >= cursor {
			t.Errorf("entry ID %d should be < cursor %d", e.ID, cursor)
		}
	}
}

func TestHistoryListNameContains(t *testing.T) {
	h := NewHistory(10)
	h.Append(HistoryEntry{Type: EventService, Name: "web-frontend"})
	h.Append(HistoryEntry{Type: EventService, Name: "api-backend"})
	h.Append(HistoryEntry{Type: EventService, Name: "WEB-PROXY"})

	result := h.List(HistoryQuery{NameContains: "web", Limit: 10})
	if len(result) != 2 {
		t.Fatalf("got %d entries, want 2", len(result))
	}

	for _, e := range result {
		lower := strings.ToLower(e.Name)
		if !strings.Contains(lower, "web") {
			t.Errorf("entry name %q does not contain 'web'", e.Name)
		}
	}
}

func TestHistoryListBeforeIDByResource(t *testing.T) {
	h := NewHistory(100)

	for i := range 6 {
		h.Append(HistoryEntry{
			Type:       EventService,
			Action:     "update",
			ResourceID: "svc1",
			Name:       fmt.Sprintf("entry-%d", i),
		})
	}

	all := h.List(HistoryQuery{ResourceID: "svc1", Limit: 10})
	if len(all) != 6 {
		t.Fatalf("got %d entries, want 6", len(all))
	}

	// all is newest-first; cursor is the 2nd newest entry
	cursor := all[1].ID
	result := h.List(HistoryQuery{ResourceID: "svc1", BeforeID: cursor, Limit: 10})

	for _, e := range result {
		if e.ID >= cursor {
			t.Errorf("entry ID %d should be < cursor %d", e.ID, cursor)
		}
		if e.ResourceID != "svc1" {
			t.Errorf("got resourceID %q, want svc1", e.ResourceID)
		}
	}

	if len(result) != 4 {
		t.Fatalf("got %d entries, want 4", len(result))
	}
}
