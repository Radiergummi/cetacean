package cluster

import (
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestParseTimeWindow(t *testing.T) {
	at := time.Date(2026, 9, 5, 14, 2, 0, 0, time.UTC)

	for _, tc := range []struct {
		name         string
		since, until string
		want         TimeWindow
		wantErr      string
	}{
		{name: "open", want: TimeWindow{}},
		{name: "both", since: "2026-09-05T14:02:00Z", until: "2026-09-05T15:02:00Z",
			want: TimeWindow{Since: at, Until: at.Add(time.Hour)}},
		{name: "offset", since: "2026-09-05T16:02:00+02:00", want: TimeWindow{Since: at}},
		{name: "lower case", since: "2026-09-05t14:02:00z", want: TimeWindow{Since: at}},
		{name: "bad since", since: "yesterday", wantErr: `invalid "since" parameter`},
		{name: "bad until", until: "1h", wantErr: `invalid "until" parameter`},
		{name: "date only", since: "2026-09-05", wantErr: `invalid "since" parameter`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTimeWindow(tc.since, tc.until)

			if tc.wantErr != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one starting %s", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !got.Since.Equal(tc.want.Since) || !got.Until.Equal(tc.want.Until) {
				t.Errorf("window = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTimeWindowOverlaps(t *testing.T) {
	base := time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)
	window := TimeWindow{Since: base, Until: base.Add(time.Hour)}

	for _, tc := range []struct {
		name     string
		from, to time.Time
		want     bool
	}{
		{"moment at since", base, base, false},
		{"moment at until", base.Add(time.Hour), base.Add(time.Hour), true},
		{"moment inside", base.Add(time.Minute), base.Add(time.Minute), true},
		{"range ending at since", base.Add(-time.Hour), base, false},
		{"range straddling since", base.Add(-time.Hour), base.Add(time.Second), true},
		{"range starting at until", base.Add(time.Hour), base.Add(2 * time.Hour), true},
		{"range after", base.Add(61 * time.Minute), base.Add(2 * time.Hour), false},
		{"range covering", base.Add(-time.Hour), base.Add(2 * time.Hour), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := window.Overlaps(tc.from, tc.to); got != tc.want {
				t.Errorf("Overlaps = %t, want %t", got, tc.want)
			}
		})
	}

	if !(TimeWindow{}).Overlaps(base, base) {
		t.Error("an open window holds every moment")
	}
}

// The history ring applies the window itself, so an item the window holds and
// an entry the ring returns for it must be the same set, a gap's whole range
// included. A gap's start may precede the entry before it; its end may not.
func TestTimeWindowAgreesWithTheHistoryRing(t *testing.T) {
	base := time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)
	at := func(seconds int) time.Time { return base.Add(time.Duration(seconds) * time.Second) }

	history := cache.NewHistory(16)
	for _, e := range []cache.HistoryEntry{
		{Type: cache.EventService, Name: "web", Timestamp: at(0)},
		{Kind: cache.KindGap, Timestamp: at(30), Until: at(90)},
		{Type: cache.EventService, Name: "web", Timestamp: at(120)},
		{Kind: cache.KindGap, Timestamp: at(60), Until: at(210)},
		{Type: cache.EventService, Name: "web", Timestamp: at(240)},
		{Kind: cache.KindGap, Timestamp: at(210), Until: at(300)},
	} {
		history.Append(e)
	}

	for _, window := range []TimeWindow{
		{Since: at(60), Until: at(180)},
		{Since: at(90), Until: at(90)},
		{Since: at(150)},
		{Until: at(30)},
		{Since: at(210), Until: at(210)},
	} {
		returned := make(map[uint64]bool)
		for _, e := range history.List(cache.HistoryQuery{
			Limit:  history.Size(),
			After:  window.Since,
			Before: window.Until,
		}) {
			returned[e.ID] = true
		}

		for _, e := range history.List(cache.HistoryQuery{Limit: history.Size()}) {
			end := e.Until
			if end.IsZero() {
				end = e.Timestamp
			}

			if held := window.Overlaps(e.Timestamp, end); held != returned[e.ID] {
				t.Errorf("window %v, entry %d: window holds it %t, ring returned it %t",
					window, e.ID, held, returned[e.ID])
			}
		}
	}
}
