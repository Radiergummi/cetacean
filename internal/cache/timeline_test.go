package cache

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

var timelineBase = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func timelineCache(t *testing.T, ringSize int) *Cache {
	t.Helper()

	c := New(nil, WithHistorySize(ringSize))
	c.RestoreAnnotations(&fakeAnnotationStore{}, nil)

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Labels = map[string]string{StackLabel: "shop"}
	c.SetService(svc)

	return c
}

func (c *Cache) change(t *testing.T, at time.Duration) uint64 {
	t.Helper()

	return c.history.Append(HistoryEntry{
		Type: EventService, ResourceID: "svc1", Name: "web", Timestamp: timelineBase.Add(at),
	})
}

func (c *Cache) note(t *testing.T, a Annotation) uint64 {
	t.Helper()

	if a.Text == "" {
		a.Text = "note"
	}
	if a.Timestamp.IsZero() {
		a.Timestamp = timelineBase
	}

	created, err := c.CreateAnnotation(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}

	return created.ID
}

func timelineIDs(entries []HistoryEntry) []uint64 {
	ids := make([]uint64, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}

	return ids
}

func TestTimeline_MergesByIDAndPagesAcrossBoth(t *testing.T) {
	c := timelineCache(t, 100)
	created := c.history.Count()
	note := c.note(t, Annotation{})
	second := c.change(t, time.Minute)
	third := c.change(t, 2*time.Minute)

	first := c.Timeline(HistoryQuery{Limit: 2})
	if got := timelineIDs(first); !slices.Equal(got, []uint64{third, second}) {
		t.Fatalf("first page = %v, want [%d %d]", got, third, second)
	}

	next := c.Timeline(HistoryQuery{Limit: 2, BeforeID: second})
	if got := timelineIDs(next); !slices.Equal(got, []uint64{note, created}) {
		t.Fatalf("second page = %v, want [%d %d]", got, note, created)
	}
	if next[0].Kind != KindAnnotation || next[0].Text != "note" {
		t.Fatalf("annotation entry = %+v", next[0])
	}

	// A cursor on the annotation pages on, through the per-resource index too.
	last := c.Timeline(HistoryQuery{Limit: 2, BeforeID: note, ResourceID: "svc1"})
	if got := timelineIDs(last); !slices.Equal(got, []uint64{created}) {
		t.Fatalf("page after the annotation = %v, want [%d]", got, created)
	}
}

func TestTimeline_ListsAnnotationsOlderThanTheRing(t *testing.T) {
	c := timelineCache(t, 4)
	note := c.note(t, Annotation{})
	for i := range 6 {
		c.change(t, time.Duration(i)*time.Minute)
	}

	all := c.Timeline(HistoryQuery{Limit: 10})
	if got := all[len(all)-1]; got.ID != note || len(all) != 5 {
		t.Fatalf(
			"timeline = %v, want the four ring entries and then the annotation",
			timelineIDs(all),
		)
	}
}

func TestTimeline_AnEditKeepsThePosition(t *testing.T) {
	c := timelineCache(t, 100)
	note := c.note(t, Annotation{})
	c.change(t, time.Minute)

	_, err := c.UpdateAnnotation(context.Background(), note, func(a *Annotation) error {
		a.Text = "edited"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	got := c.Timeline(HistoryQuery{Kind: KindAnnotation})
	if len(got) != 1 || got[0].ID != note || got[0].Text != "edited" || got[0].Updated.IsZero() {
		t.Fatalf("annotations = %+v, want the edited one at its creation ID", got)
	}
}

func TestTimeline_FiltersAnnotations(t *testing.T) {
	c := timelineCache(t, 100)
	created := c.history.Count()
	onService := c.note(t, Annotation{
		Text:      "Load test",
		Tags:      []string{"load-test"},
		Resources: []AnnotationResource{{Type: EventService, ID: "svc1", Name: "web"}},
	})
	onStack := c.note(t, Annotation{
		Resources: []AnnotationResource{{Type: EventStack, ID: "shop", Name: "shop"}},
	})
	onNode := c.note(t, Annotation{
		Resources: []AnnotationResource{{Type: EventNode, ID: "node1", Name: "n1"}},
	})
	clusterWide := c.note(t, Annotation{})
	backdatedRange := c.note(t, Annotation{
		Timestamp: timelineBase.Add(-3 * time.Hour),
		Until:     timelineBase.Add(-time.Hour),
	})
	change := c.change(t, 0)

	tests := []struct {
		name string
		q    HistoryQuery
		want []uint64
	}{
		{
			"a service, its stack and the cluster",
			HistoryQuery{ResourceID: "svc1", Types: []EventType{EventService, EventTask}},
			[]uint64{change, backdatedRange, clusterWide, onStack, onService, created},
		},
		{"a type", HistoryQuery{Type: EventNode}, []uint64{backdatedRange, clusterWide, onNode}},
		{"a tag", HistoryQuery{Tag: "load-test"}, []uint64{onService}},
		{"search on the text", HistoryQuery{NameContains: "load"}, []uint64{onService}},
		{
			"a window overlapping the range",
			HistoryQuery{
				Kind:   KindAnnotation,
				After:  timelineBase.Add(-2 * time.Hour),
				Before: timelineBase.Add(-90 * time.Minute),
			},
			[]uint64{backdatedRange},
		},
		{
			"a window after the range",
			HistoryQuery{Kind: KindAnnotation, After: timelineBase.Add(-time.Hour)},
			[]uint64{clusterWide, onNode, onStack, onService},
		},
		{"changes alone", HistoryQuery{Kind: KindChange}, []uint64{change, created}},
		{
			"what the reader may read",
			HistoryQuery{
				Kind:               KindAnnotation,
				ReadableAnnotation: func(a Annotation) bool { return len(a.Resources) > 0 },
			},
			[]uint64{onNode, onStack, onService},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := timelineIDs(c.Timeline(tt.q)); !slices.Equal(got, tt.want) {
				t.Errorf("Timeline = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnnotationWrites_ReachListenersAndTheRing(t *testing.T) {
	c := timelineCache(t, 100)

	var events []Event
	c.AddOnChangeListener(func(e Event) { events = append(events, e) })

	onService := []AnnotationResource{{Type: EventService, ID: "svc1", Name: "web"}}
	id := c.note(t, Annotation{Text: "load test", Resources: onService})
	if _, err := c.UpdateAnnotation(context.Background(), id, func(a *Annotation) error {
		a.Text = "stopped early"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteAnnotation(context.Background(), id, func(Annotation) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	items, _ := c.history.Since(id - 1)
	if len(events) != 3 || len(items) != 3 {
		t.Fatalf("%d events and %d ring items, want 3 of each", len(events), len(items))
	}

	for i, want := range []struct{ action, text string }{
		{"create", "load test"}, {"update", "stopped early"}, {"remove", "stopped early"},
	} {
		e, item := events[i], items[i]
		a, ok := e.Resource.(Annotation)
		if e.Type != EventAnnotation || e.Action != want.action || !ok || a.Text != want.text ||
			len(a.Resources) != 1 || e.HistoryID != item.ID {
			t.Errorf(
				"event %d = %+v, want %s carrying %q at %d",
				i,
				e,
				want.action,
				want.text,
				item.ID,
			)
		}
		if item.Annotation == nil || item.Annotation.Text != want.text {
			t.Errorf("ring item %d = %+v, want the annotation as %s left it", i, item, want.action)
		}
	}
}

func TestAnnotationOn_TheTimelineAListFindsItOn(t *testing.T) {
	c := timelineCache(t, 100)

	for _, tt := range []struct {
		name      string
		resources []AnnotationResource
		id        string
		want      bool
	}{
		{"cluster-wide", nil, "node9", true},
		{"named", []AnnotationResource{{Type: EventNode, ID: "node1"}}, "node1", true},
		{"another", []AnnotationResource{{Type: EventNode, ID: "node1"}}, "node2", false},
		{"its service's stack", []AnnotationResource{{Type: EventStack, ID: "shop"}}, "svc1", true},
		{"the stack itself", []AnnotationResource{{Type: EventStack, ID: "shop"}}, "shop", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.AnnotationOn(Annotation{Resources: tt.resources}, tt.id); got != tt.want {
				t.Errorf("AnnotationOn(%s) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
