package cache

import (
	"fmt"
	"slices"
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

// A slot reused by another entry about the same service leaves two index
// references to it; the stale one must not yield the newer entry again.
func TestHistory_ReusedSlotIsListedOnce(t *testing.T) {
	h := NewHistory(4)
	task := func(id string) HistoryEntry {
		return HistoryEntry{Type: "task", ResourceID: id, ServiceID: "svc"}
	}
	h.Append(task("t1"))
	h.Append(task("t2"))
	h.Append(HistoryEntry{Type: "service", ResourceID: "other"})
	h.Append(HistoryEntry{Type: "service", ResourceID: "other"})
	h.Append(task("t3"))

	ids := func(q HistoryQuery) []uint64 {
		var out []uint64
		for _, e := range h.List(q) {
			out = append(out, e.ID)
		}

		return out
	}

	if got := ids(HistoryQuery{ResourceID: "svc", Limit: 3}); !slices.Equal(got, []uint64{5, 2}) {
		t.Errorf("svc timeline = %v, want [5 2]", got)
	}
	if got := ids(
		HistoryQuery{ResourceID: "svc", Limit: 2, BeforeID: 5},
	); !slices.Equal(
		got,
		[]uint64{2},
	) {
		t.Errorf("svc page before 5 = %v, want [2]", got)
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

// A cursor of zero is a position like any other: once the ring has wrapped,
// the entries right after it are gone and the replay cannot be complete.
func TestHistory_Since_ZeroAfterWrap(t *testing.T) {
	h := NewHistory(3)

	for i := range 3 {
		h.Append(HistoryEntry{Type: "service", Name: names[i]})
	}

	if entries, ok := h.Since(0); !ok || len(entries) != 3 {
		t.Fatalf("full but unwrapped ring: Since(0) = %d entries, ok=%v; want 3, true",
			len(entries), ok)
	}

	h.Append(HistoryEntry{Type: "service", Name: names[3]})

	if _, ok := h.Since(0); ok {
		t.Fatal("Since(0) after the ring wrapped: ok=true, want false")
	}

	// The ring holds 2..4, so a cursor at 1 misses nothing.
	if entries, ok := h.Since(1); !ok || len(entries) != 3 {
		t.Fatalf("Since(1) = %d entries, ok=%v; want 3, true", len(entries), ok)
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

// Pins HistoryQuery.After on the indexed and the scanning path alike. get_events
// reads the whole ring so its filters are not starved by a page of task churn,
// which makes the time bound the one worth pushing into the walk — and one
// honoured on only one path would make a single query mean two things.
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

// The per-resource index holds only indexRingSize entries, so a query asking for
// more has to come off the main ring. Answering out of the index would let the
// caller report those entries as everything the resource ever did, which is
// exactly what get_events computes its `truncated` flag from.
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

// A service's timeline is mostly its tasks: a crash loop changes nothing on
// the service itself. Both the index and the scan must return them.
func TestListByResourceIncludesTheServicesTasks(t *testing.T) {
	h := NewHistory(10000)

	for range indexRingSize * 2 {
		h.Append(HistoryEntry{Type: EventTask, ResourceID: "t", ServiceID: "svc1"})
		h.Append(HistoryEntry{Type: EventTask, ResourceID: "u", ServiceID: "svc2"})
	}
	h.Append(HistoryEntry{Type: EventService, ResourceID: "svc1"})

	for _, limit := range []int{10, 500} {
		got := h.List(HistoryQuery{ResourceID: "svc1", Limit: limit})

		if want := min(limit, indexRingSize*2+1); len(got) != want {
			t.Errorf("limit %d: got %d entries, want %d", limit, len(got), want)
		}

		for _, e := range got {
			if e.ResourceID != "svc1" && e.ServiceID != "svc1" {
				t.Fatalf("limit %d: entry for %q leaked into svc1", limit, e.ResourceID)
			}
		}
	}

	if got := h.List(HistoryQuery{ResourceID: "t", Limit: 500}); len(got) != indexRingSize*2 {
		t.Errorf("a task's own timeline: got %d entries, want %d", len(got), indexRingSize*2)
	}
}

// Paging a single resource with a cursor must not stop at the index window
// either. The Atom detail feeds page well under indexRingSize, so gating the
// index on the limit alone left them reporting a resource's history exhausted
// after one window, on a number the caller happened to pick.
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

// The ring is the whole of Cetacean's memory of what changed, and it starts with
// the process. Without the horizon in the result, "one service changed in the
// last twelve hours" and "I have only been watching for half an hour" answer
// identically — and the narrow query reports truncated:false besides.
func TestHistoryOldestReportsTheHorizonItCanAnswerFor(t *testing.T) {
	h := NewHistory(3)

	if _, ok := h.Oldest(); ok {
		t.Error("an empty ring reported a horizon")
	}

	first := time.Now().Add(-time.Hour)
	for i := range 2 {
		h.Append(HistoryEntry{
			Timestamp:  first.Add(time.Duration(i) * time.Minute),
			Type:       EventService,
			Action:     "update",
			ResourceID: "svc",
		})
	}

	oldest, ok := h.Oldest()
	if !ok {
		t.Fatal("a populated ring reported no horizon")
	}

	if !oldest.Equal(first) {
		t.Errorf("Oldest = %v, want the first entry at %v", oldest, first)
	}

	// Once it wraps, the horizon has to move with the surviving entries rather
	// than stay at the first thing ever recorded.
	for i := 2; i < 6; i++ {
		h.Append(HistoryEntry{
			Timestamp:  first.Add(time.Duration(i) * time.Minute),
			Type:       EventService,
			Action:     "update",
			ResourceID: "svc",
		})
	}

	oldest, ok = h.Oldest()
	if !ok {
		t.Fatal("a wrapped ring reported no horizon")
	}

	if want := first.Add(3 * time.Minute); !oldest.Equal(want) {
		t.Errorf("Oldest = %v, want %v — the horizon must follow the ring", oldest, want)
	}
}

// A resource whose every entry the ring has overwritten keeps no index: a
// crash-looping service mints a task ID per restart, and each would otherwise
// hold its index ring for the life of the process.
func TestHistoryPrunesIndexOfOverwrittenResources(t *testing.T) {
	const size = 8
	h := NewHistory(size)

	for i := range 100 {
		h.Append(HistoryEntry{
			Type:       EventTask,
			Action:     "update",
			ResourceID: fmt.Sprintf("task-%d", i),
		})
	}

	if got := len(h.byResource); got > size {
		t.Errorf("byResource holds %d resources, want at most the ring's %d", got, size)
	}

	// A pruned resource still answers as having nothing, and a surviving one
	// still answers from its index.
	if got := h.List(HistoryQuery{ResourceID: "task-0"}); len(got) != 0 {
		t.Errorf("List(task-0) = %d entries, want none once overwritten", len(got))
	}

	if got := h.List(HistoryQuery{ResourceID: "task-99"}); len(got) != 1 {
		t.Errorf("List(task-99) = %d entries, want 1", len(got))
	}
}

// A task entry is indexed under its service too, so overwriting it must prune
// that index as well, or every service that ever ran a task keeps one.
func TestHistoryPrunesServiceIndexOfOverwrittenTasks(t *testing.T) {
	const size = 8
	h := NewHistory(size)

	for i := range 100 {
		h.Append(HistoryEntry{
			Type:       EventTask,
			Action:     "update",
			ResourceID: fmt.Sprintf("task-%d", i),
			ServiceID:  fmt.Sprintf("svc-%d", i),
		})
	}

	if got := len(h.byResource); got > 2*size {
		t.Errorf("byResource holds %d IDs, want at most the ring's tasks and services", got)
	}

	if _, ok := h.byResource["svc-0"]; ok {
		t.Error("svc-0's index outlived its last entry")
	}
}

// A resource that recurs keeps its index while any of its entries survive.
func TestHistoryKeepsIndexWhileAnEntrySurvives(t *testing.T) {
	h := NewHistory(4)

	h.Append(HistoryEntry{ResourceID: "svc"})
	h.Append(HistoryEntry{ResourceID: "a"})
	h.Append(HistoryEntry{ResourceID: "svc"})
	h.Append(HistoryEntry{ResourceID: "b"})
	h.Append(HistoryEntry{ResourceID: "c"}) // overwrites svc's first entry only

	if got := h.List(HistoryQuery{ResourceID: "svc"}); len(got) != 1 {
		t.Errorf("List(svc) = %d entries, want the 1 that survives", len(got))
	}

	if _, ok := h.byResource["svc"]; !ok {
		t.Error("svc's index was pruned while one of its entries survives")
	}
}

func TestHistory_Since_OverwrittenReturnsSurvivors(t *testing.T) {
	h := NewHistory(3)

	for i := range 5 {
		h.Append(HistoryEntry{Type: "service", Name: names[i]})
	}

	entries, ok := h.Since(1)
	if ok {
		t.Fatal("expected ok=false for overwritten ID")
	}

	if got := entryIDs(entries); !slices.Equal(got, []uint64{3, 4, 5}) {
		t.Fatalf("Since(1) = %v, want the survivors [3 4 5]", got)
	}
}

func TestHistory_Restore(t *testing.T) {
	h := NewHistory(10)

	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	h.Restore([]HistoryEntry{
		{ID: 4, Timestamp: at, Type: EventService, ResourceID: "s1", Name: "web"},
		{ID: 9, Timestamp: at, Type: EventTask, ResourceID: "t1", ServiceID: "s1"},
	})

	if h.Count() != 9 {
		t.Fatalf("Count() = %d, want the last restored ID 9", h.Count())
	}

	if id := h.Append(HistoryEntry{Type: EventService, ResourceID: "s1"}); id != 10 {
		t.Fatalf("Append after Restore = %d, want 10", id)
	}

	if got := entryIDs(
		h.List(HistoryQuery{ResourceID: "s1"}),
	); !slices.Equal(
		got,
		[]uint64{10, 9, 4},
	) {
		t.Fatalf("List(s1) = %v, want [10 9 4] through the rebuilt index", got)
	}
}

func TestHistory_RestoreKeepsTheNewestThatFit(t *testing.T) {
	h := NewHistory(3)

	var restored []HistoryEntry
	for id := range uint64(5) {
		restored = append(restored, HistoryEntry{ID: id + 1, Type: EventService})
	}

	h.Restore(restored)

	if got := entryIDs(h.List(HistoryQuery{})); !slices.Equal(got, []uint64{5, 4, 3}) {
		t.Fatalf("List() = %v, want the newest three", got)
	}

	if entries, ok := h.Since(2); !ok || len(entries) != 3 {
		t.Fatalf("Since(2) = %d entries, ok=%v; want 3, true", len(entries), ok)
	}
}

// IDs restored after an overrun jump, so the oldest one cannot be derived
// from the count and the size.
func TestHistory_SinceAcrossRestoredJump(t *testing.T) {
	h := NewHistory(4)

	h.Restore([]HistoryEntry{
		{ID: 2, Type: EventService},
		{ID: 3, Type: EventService},
		{ID: 50, Type: EventService},
	})
	h.Append(HistoryEntry{Type: EventService})

	entries, ok := h.Since(1)
	if !ok {
		t.Fatal("Since(1): ok=false, but the ring still holds the entry after it")
	}

	if got := entryIDs(entries); !slices.Equal(got, []uint64{2, 3, 50, 51}) {
		t.Fatalf("Since(1) = %v, want [2 3 50 51]", got)
	}
}

func entryIDs(entries []HistoryEntry) []uint64 {
	ids := make([]uint64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}

	return ids
}

func TestRecordGapGoesThroughNotify(t *testing.T) {
	var events []Event
	c := New(func(e Event) { events = append(events, e) })

	from := time.Date(2026, 10, 30, 13, 0, 0, 0, time.UTC)
	before := time.Now()
	c.RecordGap(from)

	entries := c.History().List(HistoryQuery{})
	if len(entries) != 1 {
		t.Fatalf("history holds %d entries, want the gap", len(entries))
	}

	gap := entries[0]
	if gap.Kind != KindGap || !gap.Timestamp.Equal(from) ||
		gap.Until.Before(before) || gap.Type != "" || gap.ResourceID != "" {
		t.Errorf("gap entry = %+v, want a gap from 13:00 to now naming no resource", gap)
	}

	if len(events) != 1 || events[0].Type != EventGap || events[0].HistoryID != gap.ID ||
		!events[0].Timestamp.Equal(gap.Timestamp) || !events[0].Until.Equal(gap.Until) {
		t.Errorf("listeners got %+v, want one gap event carrying entry %d", events, gap.ID)
	}
}

func TestAppendDefaultsToAChange(t *testing.T) {
	h := NewHistory(10)
	h.Append(HistoryEntry{Type: EventService, ResourceID: "svc1"})

	if kind := h.List(HistoryQuery{})[0].Kind; kind != KindChange {
		t.Fatalf("Kind = %q, want %q", kind, KindChange)
	}
}

// gapHistory holds a change at 12:00, a gap from 13:00 to 16:00 and a change
// at 17:00, all on or across svc1's timeline.
func gapHistory() *History {
	base := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	h := NewHistory(10)
	h.Append(HistoryEntry{
		Type: EventService, ResourceID: "svc1", Name: "web", Timestamp: base.Add(12 * time.Hour),
	})
	h.Append(HistoryEntry{
		Kind: KindGap, Timestamp: base.Add(13 * time.Hour), Until: base.Add(16 * time.Hour),
	})
	h.Append(HistoryEntry{
		Type: EventService, ResourceID: "svc1", Name: "web", Timestamp: base.Add(17 * time.Hour),
	})

	return h
}

func TestListGapOverlapsTheWindow(t *testing.T) {
	base := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	h := gapHistory()

	for _, tc := range []struct {
		name          string
		after, before time.Time
		want          []uint64
	}{
		{"inside the gap", base.Add(14 * time.Hour), base.Add(15 * time.Hour), []uint64{2}},
		{"from the gap's end", base.Add(16 * time.Hour), time.Time{}, []uint64{3}},
		{"just before the gap's end", base.Add(16*time.Hour - 1), time.Time{}, []uint64{3, 2}},
		{"up to the gap's start", time.Time{}, base.Add(13 * time.Hour), []uint64{2, 1}},
		{"before the gap", time.Time{}, base.Add(13*time.Hour - 1), []uint64{1}},
	} {
		for _, resource := range []string{"", "svc1"} {
			got := historyIDs(h.List(HistoryQuery{
				ResourceID: resource, After: tc.after, Before: tc.before, Limit: 10,
			}))
			if !slices.Equal(got, tc.want) {
				t.Errorf("%s, resource %q: got %v, want %v", tc.name, resource, got, tc.want)
			}
		}
	}
}

func TestGapPassesEveryResourceFilter(t *testing.T) {
	h := gapHistory()

	for _, q := range []HistoryQuery{
		{Type: EventNode},
		{ResourceID: "other"},
		{NameContains: "nothing"},
		{Types: []EventType{EventTask}},
		{Kind: KindGap},
		{Kind: KindGap, ResourceID: "svc1"},
	} {
		if got := historyIDs(h.List(q)); !slices.Equal(got, []uint64{2}) {
			t.Errorf("List(%+v) = %v, want only the gap", q, got)
		}
	}

	for _, q := range []HistoryQuery{
		{Kind: KindChange},
		{Kind: KindChange, ResourceID: "svc1"},
		{Visible: func(e HistoryEntry) bool { return e.Kind != KindGap }},
	} {
		if got := historyIDs(h.List(q)); !slices.Equal(got, []uint64{3, 1}) {
			t.Errorf("List(%+v) = %v, want the changes only", q, got)
		}
	}
}

// The index merges gaps into a resource's entries; it must answer exactly
// what a scan of the ring does, for every page, cursor and wrapped index.
func TestListByResourceMergesGapsLikeAScan(t *testing.T) {
	h := NewHistory(2000)
	for i := range 600 {
		switch {
		case i%7 == 0:
			h.Append(HistoryEntry{Kind: KindGap})
		case i%3 == 0:
			h.Append(HistoryEntry{Type: EventService, ResourceID: "svc1"})
		default:
			h.Append(HistoryEntry{Type: EventService, ResourceID: "svc2"})
		}
	}

	scan := func(q HistoryQuery) []uint64 {
		var want []uint64
		pastCursor := q.BeforeID == 0
		for _, e := range h.List(HistoryQuery{Limit: h.Size()}) {
			if !pastCursor {
				pastCursor = e.ID == q.BeforeID
				continue
			}
			if q.matches(e) && len(want) < q.Limit {
				want = append(want, e.ID)
			}
		}
		return want
	}

	for _, kind := range []string{"", KindChange, KindGap} {
		for _, limit := range []int{1, 10, 50, 200} {
			q := HistoryQuery{ResourceID: "svc1", Kind: kind, Limit: limit}
			for page := range 30 {
				got := historyIDs(h.List(q))
				if want := scan(q); !slices.Equal(got, want) {
					t.Fatalf("kind %q, limit %d, page %d: got %v, want %v",
						kind, limit, page, got, want)
				}
				if len(got) < limit {
					break
				}
				q.BeforeID = got[len(got)-1]
			}
		}
	}
}

func TestRestoreIndexesGaps(t *testing.T) {
	h := NewHistory(10)
	h.Restore([]HistoryEntry{
		{ID: 4, Kind: KindChange, Type: EventService, ResourceID: "svc1"},
		{ID: 5, Kind: KindGap},
	})

	got := historyIDs(h.List(HistoryQuery{ResourceID: "svc1"}))
	if !slices.Equal(got, []uint64{5, 4}) {
		t.Fatalf("svc1's timeline after a restore = %v, want [5 4]", got)
	}
}

func historyIDs(entries []HistoryEntry) []uint64 {
	result := make([]uint64, 0, len(entries))
	for _, e := range entries {
		result = append(result, e.ID)
	}

	return result
}

// A change recorded before a gap must not end after it, or a window starting
// between the two would end the walk at the gap and skip the change.
func TestGapEndsNoEarlierThanTheChangesBeforeIt(t *testing.T) {
	c := New(nil)
	c.History().Append(HistoryEntry{Type: EventService, ResourceID: "svc1"})
	c.RecordGap(time.Now().Add(-time.Hour))

	entries := c.History().List(HistoryQuery{})
	if gap, change := entries[0], entries[1]; gap.Until.Before(change.Timestamp) {
		t.Fatalf("gap ends %v, before the change it follows at %v", gap.Until, change.Timestamp)
	}
}
