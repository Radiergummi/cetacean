package api

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

var (
	gapFrom  = time.Date(2026, 10, 30, 13, 0, 0, 0, time.UTC)
	gapUntil = time.Date(2026, 10, 30, 16, 0, 0, 0, time.UTC)
)

// gapCache records "web" being created, then a gap from 13:00 to 16:00, then
// "api" being created: entries 1, 2 and 3.
func gapCache() *cache.Cache {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "s1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.History().Append(cache.HistoryEntry{Kind: cache.KindGap, Timestamp: gapFrom, Until: gapUntil})
	c.SetService(swarm.Service{
		ID:   "s2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api"}},
	})

	return c
}

type gapPageItem struct {
	AtID      string `json:"@id"`
	AtType    string `json:"@type"`
	ID        uint64 `json:"id"`
	Kind      string `json:"kind"`
	Timestamp string `json:"timestamp"`
	Until     string `json:"until"`
	Name      string `json:"name"`
}

func gapPageItems(t *testing.T, body string) []gapPageItem {
	t.Helper()

	var page struct {
		Items []gapPageItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("history page: %v\n%s", err, body)
	}

	return page.Items
}

func itemIDs(items []gapPageItem) []uint64 {
	ids := make([]uint64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}

	return ids
}

func TestHistoryListsAGapAsAGap(t *testing.T) {
	items := gapPageItems(t, getHistory(t, newTestRouterWithCache(t, gapCache()), "/history"))

	if got := itemIDs(items); !slices.Equal(got, []uint64{3, 2, 1}) {
		t.Fatalf("items = %v, want [3 2 1]", got)
	}

	gap := items[1]
	want := gapPageItem{
		AtID:      "/history#2",
		AtType:    "Gap",
		ID:        2,
		Kind:      cache.KindGap,
		Timestamp: "2026-10-30T13:00:00Z",
		Until:     "2026-10-30T16:00:00Z",
	}
	if gap != want {
		t.Errorf("gap = %+v, want %+v", gap, want)
	}

	if items[0].AtType != "HistoryEntry" || items[0].Kind != cache.KindChange ||
		items[0].Until != "" {
		t.Errorf("change = %+v, want a HistoryEntry of kind change with no until", items[0])
	}
}

func TestHistoryFiltersAroundAGap(t *testing.T) {
	router := newTestRouterWithCache(t, gapCache())

	for _, tc := range []struct {
		query string
		want  []uint64
	}{
		{"kind=change", []uint64{3, 1}},
		{"kind=gap", []uint64{2}},
		{"kind=annotation", []uint64{}},
		{"type=node", []uint64{2}},
		{"type=service&resourceId=s1", []uint64{2, 1}},
		{"search=api", []uint64{3, 2}},
		{"since=2026-10-30T14:00:00Z&until=2026-10-30T15:00:00Z", []uint64{2}},
	} {
		got := itemIDs(gapPageItems(t, getHistory(t, router, "/history?"+tc.query)))
		if !slices.Equal(got, tc.want) {
			t.Errorf("/history?%s = %v, want %v", tc.query, got, tc.want)
		}
	}
}

// A gap names no resource, so a caller who may read only some of the
// cluster still learns nothing was watched.
func TestHistoryShowsAGapToEveryReader(t *testing.T) {
	h := newTestHandlers(t, withCache(gapCache()), withACL(aclPolicyForAlice()))

	req := httptest.NewRequest(http.MethodGet, "/history?kind=gap", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "alice"}))
	rec := httptest.NewRecorder()
	h.HandleHistory(rec, req)

	if got := itemIDs(gapPageItems(t, rec.Body.String())); !slices.Equal(got, []uint64{2}) {
		t.Fatalf("alice's gaps = %v, want [2]; body: %s", got, rec.Body)
	}
}

func TestHistoryCSVCarriesAGapAsARow(t *testing.T) {
	records := csvRecords(t, getHistory(t, newTestRouterWithCache(t, gapCache()), "/history.csv"))

	if got := records[0][10:]; !slices.Equal(got, []string{"kind", "until"}) {
		t.Fatalf("trailing columns = %v, want [kind until]", got)
	}

	gap := records[2]
	if gap[0] != "2026-10-30T13:00:00Z" || gap[10] != "gap" || gap[11] != "2026-10-30T16:00:00Z" {
		t.Errorf("gap row = %v, want it from 13:00 to 16:00 of kind gap", gap)
	}

	if change := records[1]; change[10] != "change" || change[11] != "" {
		t.Errorf("change row ends %v, want change and no until", change[10:])
	}
}

func TestHistoryFeedsTitleAGap(t *testing.T) {
	router := newTestRouterWithCache(t, gapCache())
	title := "Nothing recorded from 2026-10-30T13:00:00Z to 2026-10-30T16:00:00Z"

	var atom struct {
		Entries []struct {
			Title   string `xml:"title"`
			Updated string `xml:"updated"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal([]byte(getHistory(t, router, "/history.atom")), &atom); err != nil {
		t.Fatal(err)
	}

	if len(atom.Entries) != 3 || atom.Entries[1].Title != title ||
		atom.Entries[1].Updated != "2026-10-30T16:00:00Z" {
		t.Errorf(
			"Atom entries = %+v, want the gap titled %q, updated at its end",
			atom.Entries,
			title,
		)
	}

	var jsonFeed struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
	}
	body := getHistory(t, router, "/history.feed")
	if err := json.Unmarshal([]byte(body), &jsonFeed); err != nil {
		t.Fatalf("JSON Feed: %v\n%s", err, body)
	}

	if len(jsonFeed.Items) != 3 || jsonFeed.Items[1].Title != title {
		t.Errorf("JSON Feed items = %+v, want the gap titled %q", jsonFeed.Items, title)
	}
}

func TestEventsStreamCarriesAGap(t *testing.T) {
	t.Parallel()

	gapEvent := cache.Event{
		Type:      cache.EventGap,
		HistoryID: 7,
		Timestamp: gapFrom,
		Until:     gapUntil,
	}

	router, _, broadcaster := streamTestRouter(t, 0)

	frames := streamUntilIdle(t, router, "/events?types=service", "", func() {
		broadcaster.Broadcast(gapEvent)
	})

	if len(frames) != 1 || frames[0].Event != "gap" {
		t.Fatalf("frames = %+v, want one gap event through the types filter", frames)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(frames[0].Data), &payload); err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		"@id":       "/history#7",
		"@type":     "Gap",
		"type":      "gap",
		"action":    "",
		"id":        "",
		"timestamp": "2026-10-30T13:00:00Z",
		"until":     "2026-10-30T16:00:00Z",
	}
	for key, value := range want {
		if payload[key] != value {
			t.Errorf("payload[%q] = %v, want %v", key, payload[key], value)
		}
	}
}

// Replay reads the gap from the ring: /events sends it, while a resource
// channel, which carries resource state rather than the timeline, skips it.
func TestReplaySendsAGapOnlyOnEvents(t *testing.T) {
	t.Parallel()

	router, c, broadcaster := streamTestRouter(t, 0)
	cursor := broadcaster.EventID(c.History().Count())
	c.RecordGap(gapFrom)

	frames := streamUntilIdle(t, router, "/events", cursor, nil)
	if len(frames) == 0 || frames[0].Event != "gap" ||
		!strings.Contains(frames[0].Data, `"timestamp":"2026-10-30T13:00:00Z","until":`) {
		t.Fatalf("/events replay = %+v, want the gap with its until", frames)
	}

	if frames := streamUntilIdle(t, router, "/services", cursor, nil); len(frames) != 0 {
		t.Errorf("/services replay = %+v, want nothing", frames)
	}
}

func TestHistoryNextLinkKeepsTheKind(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestRouterWithCache(t, gapCache()).ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodGet, "/history?kind=change&limit=1", nil),
	)

	var link string
	for _, value := range rec.Header().Values("Link") {
		if strings.Contains(value, `rel="next"`) {
			link = value
		}
	}

	start, end := strings.Index(link, "<"), strings.Index(link, ">")
	if start < 0 || end < start {
		t.Fatalf("Link = %q, want a next link", link)
	}

	next, err := url.Parse(link[start+1 : end])
	if err != nil || next.Query().Get("kind") != "change" {
		t.Errorf("next link = %q, want it to keep kind=change", link)
	}
}
