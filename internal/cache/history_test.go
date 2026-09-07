package cache

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
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

// TestHistoryListAfterBoundsBothPaths pins HistoryQuery.After on the indexed
// and the scanning path alike. get_events reads the whole ring so its type and
// `until` filters are not starved by a page of task churn, which made the time
// bound the one thing worth pushing into the walk — and a bound honoured on
// only one of the two paths would make one query mean two things.
func TestHistoryListAfterBoundsBothPaths(t *testing.T) {
	h := NewHistory(10)

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := range 5 {
		h.Append(HistoryEntry{
			Type:       "service",
			Action:     "update",
			ResourceID: "svc1",
			Name:       names[i],
			Timestamp:  base.Add(time.Duration(i) * time.Minute),
		})
	}

	cutoff := base.Add(2 * time.Minute)

	// Scanning path: no ResourceID, so List walks the ring itself.
	scanned := h.List(HistoryQuery{Limit: 10, After: cutoff})
	if len(scanned) != 2 {
		t.Fatalf("scanned = %d entries, want the 2 strictly after the cutoff", len(scanned))
	}

	// Indexed path: the same bound, through listByResource.
	indexed := h.List(HistoryQuery{ResourceID: "svc1", Limit: 10, After: cutoff})
	if len(indexed) != 2 {
		t.Fatalf("indexed = %d entries, want the 2 strictly after the cutoff", len(indexed))
	}

	for _, e := range append(scanned, indexed...) {
		if !e.Timestamp.After(cutoff) {
			t.Errorf("entry %q at %v is not after the cutoff", e.Name, e.Timestamp)
		}
	}
}

// The per-resource index holds only indexRingSize entries, so a query asking
// for more than that has to come off the main ring instead. Answering out of
// the index would hand back 64 entries and let the caller report them as
// everything the resource ever did — internal/mcp's get_events computes its
// `truncated` flag from exactly this count.
func TestListByResourceBeyondTheIndexRing(t *testing.T) {
	const entries = indexRingSize * 3

	h := NewHistory(10000)

	for range entries {
		h.Append(HistoryEntry{
			Type:       EventService,
			Action:     "update",
			ResourceID: "svc1",
			Name:       "web",
		})

		// Interleave another resource, so the walk has to filter rather than
		// simply read every slot it passes.
		h.Append(HistoryEntry{
			Type:       EventService,
			Action:     "update",
			ResourceID: "svc2",
			Name:       "api",
		})
	}

	wide := h.List(HistoryQuery{ResourceID: "svc1", Limit: 500})
	if len(wide) != entries {
		t.Fatalf(
			"got %d entries, want all %d: the index cannot bound a wider read",
			len(wide),
			entries,
		)
	}

	for _, e := range wide {
		if e.ResourceID != "svc1" {
			t.Fatalf("entry for %q leaked into a query for svc1", e.ResourceID)
		}
	}

	// Newest first, as on both paths.
	for i := 1; i < len(wide); i++ {
		if wide[i-1].ID < wide[i].ID {
			t.Fatalf(
				"entry %d (id %d) is older than the one after it (id %d)",
				i,
				wide[i-1].ID,
				wide[i].ID,
			)
		}
	}

	// A query the index *can* answer still takes the fast path and agrees with
	// the scan about which entries are newest.
	narrow := h.List(HistoryQuery{ResourceID: "svc1", Limit: 10})
	if len(narrow) != 10 {
		t.Fatalf("got %d entries, want 10", len(narrow))
	}

	for i, e := range narrow {
		if e.ID != wide[i].ID {
			t.Errorf("entry %d: indexed id %d, scanned id %d", i, e.ID, wide[i].ID)
		}
	}
}

// Paging a single resource with a cursor must not stop at the index window
// either. The Atom detail feeds page at 50 — comfortably under indexRingSize —
// so gating the index on the limit alone left them reporting a resource's
// history exhausted after 64 entries, and made the behaviour depend on a
// number the caller happened to pick.
func TestListByResourcePagesPastTheIndexRingWithACursor(t *testing.T) {
	const entries = indexRingSize * 4

	h := NewHistory(10000)
	for range entries {
		h.Append(
			HistoryEntry{Type: EventService, ResourceID: "svc1", Name: "web", Action: "update"},
		)
		h.Append(
			HistoryEntry{Type: EventService, ResourceID: "svc2", Name: "api", Action: "update"},
		)
	}

	// Walk the whole history one page at a time, exactly as a feed reader does.
	const page = 50

	var (
		seen   int
		cursor uint64
	)

	for {
		got := h.List(HistoryQuery{ResourceID: "svc1", BeforeID: cursor, Limit: page})
		if len(got) == 0 {
			break
		}

		for _, e := range got {
			if e.ResourceID != "svc1" {
				t.Fatalf("entry for %q leaked into a query for svc1", e.ResourceID)
			}
		}

		seen += len(got)
		cursor = got[len(got)-1].ID

		if seen > entries {
			t.Fatalf("paging returned %d entries, more than the %d recorded", seen, entries)
		}
	}

	if seen != entries {
		t.Errorf(
			"paging reached %d of %d entries before the feed reported it exhausted",
			seen,
			entries,
		)
	}
}

// The type and upper-time bounds belong in the walk, not in the caller. A
// caller reducing the result afterwards cannot have its copy bounded: List
// would have to hand back every candidate, which on a full ring of task churn
// is ten thousand entries copied to keep a handful.
func TestListFiltersByTypesAndUpperTimeBound(t *testing.T) {
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	h := NewHistory(100)
	for i, kind := range []EventType{EventService, EventTask, EventNode, EventService} {
		h.Append(HistoryEntry{
			Type:       kind,
			Action:     "update",
			ResourceID: string(kind) + strconv.Itoa(i),
			Timestamp:  base.Add(time.Duration(i) * time.Minute),
		})
	}

	services := h.List(HistoryQuery{Types: []EventType{EventService}, Limit: 50})
	if len(services) != 2 {
		t.Fatalf("got %d entries, want the 2 service events", len(services))
	}
	for _, e := range services {
		if e.Type != EventService {
			t.Errorf("entry of type %q leaked into a service-only query", e.Type)
		}
	}

	// Several types at once, which is what the tool's `types` argument takes.
	pair := h.List(HistoryQuery{Types: []EventType{EventTask, EventNode}, Limit: 50})
	if len(pair) != 2 {
		t.Errorf("got %d entries, want the task and node events", len(pair))
	}

	// Before is inclusive at its own instant and cannot end the walk, since
	// entries are visited newest-first.
	upTo := h.List(HistoryQuery{Before: base.Add(time.Minute), Limit: 50})
	if len(upTo) != 2 {
		t.Fatalf("got %d entries, want the 2 at or before the bound", len(upTo))
	}
	for _, e := range upTo {
		if e.Timestamp.After(base.Add(time.Minute)) {
			t.Errorf("entry at %v is after the upper bound", e.Timestamp)
		}
	}

	// An unrecognised type matches nothing rather than everything.
	if got := h.List(HistoryQuery{Types: []EventType{"nonsense"}, Limit: 50}); len(got) != 0 {
		t.Errorf("got %d entries for an unknown type, want none", len(got))
	}
}

// Both list paths must honour the new filters, or one query means two things
// depending on whether a resource was named.
func TestListByResourceHonoursTypesAndUpperTimeBound(t *testing.T) {
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	h := NewHistory(100)
	h.Append(HistoryEntry{Type: EventService, ResourceID: "svc1", Timestamp: base})
	h.Append(HistoryEntry{Type: EventTask, ResourceID: "svc1", Timestamp: base.Add(time.Minute)})

	byType := h.List(HistoryQuery{
		ResourceID: "svc1",
		Types:      []EventType{EventService},
		Limit:      10,
	})
	if len(byType) != 1 || byType[0].Type != EventService {
		t.Errorf("got %+v, want only the service entry", byType)
	}

	byTime := h.List(HistoryQuery{ResourceID: "svc1", Before: base, Limit: 10})
	if len(byTime) != 1 || byTime[0].Timestamp.After(base) {
		t.Errorf("got %+v, want only the entry at or before the bound", byTime)
	}
}
