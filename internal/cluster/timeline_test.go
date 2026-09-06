package cluster

import (
	"testing"
	"time"
)

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

// SortTimeline compares At as a string, so the format that produces it must be
// fixed width. time.RFC3339Nano trims trailing zeros, which makes ":00Z" and
// ":00.5Z" compare on 'Z' against '.' — sorting the earlier one as the newer.
func TestTimelineTimeIsFixedWidthSoStringOrderIsTimeOrder(t *testing.T) {
	base := time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)

	entries := []TimelineEntry{
		{At: TimelineTime(base), Kind: "change", Message: "first"},
		{At: TimelineTime(base.Add(500 * time.Millisecond)), Kind: "change", Message: "second"},
		{At: TimelineTime(base.Add(520 * time.Millisecond)), Kind: "change", Message: "third"},
	}

	SortTimeline(entries)

	want := []string{"third", "second", "first"}
	for i, w := range want {
		if entries[i].Message != w {
			t.Errorf("entry %d = %q (%s), want %q", i, entries[i].Message, entries[i].At, w)
		}
	}
}

// The same instant in another zone has to render identically, or a cursor
// stops being comparable across entries.
func TestTimelineTimeNormalisesToUTC(t *testing.T) {
	zone := time.FixedZone("CEST", 2*60*60)

	utc := TimelineTime(time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC))
	local := TimelineTime(time.Date(2026, 9, 5, 16, 0, 0, 0, zone))

	if utc != local {
		t.Errorf("same instant rendered as %q and %q", utc, local)
	}
}
