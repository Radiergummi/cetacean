package cluster

import "testing"

// A timeline is read newest-first, and a log line and a change event that
// happened in the same second must be orderable against each other — that is
// the whole reason the two share a shape.
func TestSortTimelineOrdersNewestFirst(t *testing.T) {
	entries := []TimelineEntry{
		{At: "2026-09-05T14:00:00Z", Kind: "change", Message: "older"},
		{At: "2026-09-05T14:02:00Z", Kind: "log", Message: "newer"},
		{At: "2026-09-05T14:01:00Z", Kind: "change", Message: "middle"},
	}

	SortTimeline(entries)

	want := []string{"newer", "middle", "older"}
	for i, w := range want {
		if entries[i].Message != w {
			t.Errorf("entry %d = %q, want %q", i, entries[i].Message, w)
		}
	}
}

// Ties break on Kind then Message so the order is total: an ETag over the
// result must not change between two calls that saw the same data.
func TestSortTimelineIsDeterministicOnTies(t *testing.T) {
	a := []TimelineEntry{
		{At: "2026-09-05T14:00:00Z", Kind: "log", Message: "b"},
		{At: "2026-09-05T14:00:00Z", Kind: "change", Message: "a"},
	}
	b := []TimelineEntry{
		{At: "2026-09-05T14:00:00Z", Kind: "change", Message: "a"},
		{At: "2026-09-05T14:00:00Z", Kind: "log", Message: "b"},
	}

	SortTimeline(a)
	SortTimeline(b)

	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("entry %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}
