package mcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// alicesNote is a note alice wrote on the service, which bob may read.
func alicesNote(t *testing.T, c *cache.Cache, tags ...string) cache.Annotation {
	t.Helper()

	a, err := c.CreateAnnotation(context.Background(), cache.Annotation{
		Timestamp: time.Date(2026, 11, 1, 14, 0, 0, 0, time.UTC),
		Text:      "load test",
		Tags:      tags,
		Resources: []cache.AnnotationResource{{Type: cache.EventService, ID: "svc1", Name: "web"}},
		Actor:     &cache.Actor{Subject: "alice", Provider: "headers"},
	})
	if err != nil {
		t.Fatal(err)
	}

	return a
}

func TestAnnotationToolsAreWithheldWithoutTheTimeline(t *testing.T) {
	for _, timeline := range []bool{true, false} {
		srv, _ := annotationServer(t, timeline)

		for _, name := range []string{"add_annotation", "update_annotation", "delete_annotation"} {
			if _, ok := srv.findTool(name); ok != timeline {
				t.Errorf("timeline %v: %s registered = %v", timeline, name, ok)
			}
		}
	}
}

func TestUpdateAnnotationChangesOnlyWhatItIsGiven(t *testing.T) {
	srv, c := annotationServer(t, true)
	note := alicesNote(t, c, "load")

	result := callTool(t, annotatingAs("alice", srv.Handler()),
		`{"name":"update_annotation","arguments":{"id":`+strconv.FormatUint(note.ID, 10)+
			`,"text":"load test, stopped early","tags":[],"version":`+
			strconv.FormatUint(note.Version, 10)+`}}`)

	var got annotationResult
	if err := json.Unmarshal(result.StructuredContent, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, result.StructuredContent)
	}

	if got.Text != "load test, stopped early" || len(got.Tags) != 0 ||
		got.At != "2026-11-01T14:00:00.000000000Z" || len(got.Resources) != 1 {
		t.Errorf("result = %+v, want the text changed, the tags cleared and the rest kept", got)
	}
	if got.Version <= note.Version || got.Editor == nil || got.Editor.Subject != "alice" ||
		got.Updated == "" {
		t.Errorf("result = %+v, want a new version edited by alice", got)
	}
}

func TestUpdateAnnotationRefusals(t *testing.T) {
	srv, c := annotationServer(t, true)
	note := alicesNote(t, c)
	id := strconv.FormatUint(note.ID, 10)

	for _, tt := range []struct {
		name, subject, args, want string
	}{
		{"a stale version", "alice", `{"id":` + id + `,"text":"x","version":1}`, "now at version"},
		{"a reader who did not write it", "bob", `{"id":` + id + `,"text":"x"}`, "write access denied"},
		{"no such note", "alice", `{"id":999999,"text":"x"}`, "annotation 999999 not found"},
		{"an empty start", "alice", `{"id":` + id + `,"at":""}`, "at: cannot be removed"},
		{"an end that is not a string", "alice", `{"id":` + id + `,"until":0}`, "until: must be"},
		{
			"a range ending before it starts",
			"alice",
			`{"id":` + id + `,"until":"2026-11-01T13:00:00Z"}`,
			"until: must be after the start",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := callToolExpectingError(t, annotatingAs(tt.subject, srv.Handler()),
				`{"name":"update_annotation","arguments":`+tt.args+`}`)
			if !strings.Contains(raw, tt.want) {
				t.Errorf("error = %s, want it to mention %q", raw, tt.want)
			}
		})
	}
}

func TestDeleteAnnotation(t *testing.T) {
	srv, c := annotationServer(t, true)
	note := alicesNote(t, c)
	id := strconv.FormatUint(note.ID, 10)

	raw := callToolExpectingError(t, annotatingAs("bob", srv.Handler()),
		`{"name":"delete_annotation","arguments":{"id":`+id+`}}`)
	if !strings.Contains(raw, "write access denied") {
		t.Fatalf("bob deletes: %s, want a refusal", raw)
	}

	callTool(t, annotatingAs("alice", srv.Handler()),
		`{"name":"delete_annotation","arguments":{"id":`+id+`,"version":`+
			strconv.FormatUint(note.Version, 10)+`}}`)

	if _, ok := c.Annotation(note.ID); ok {
		t.Fatal("the note is still there")
	}
}

func TestAnnotationResources(t *testing.T) {
	srv, c := annotationServer(t, true)
	onService := alicesNote(t, c, "load")
	onNode, err := c.CreateAnnotation(context.Background(), cache.Annotation{
		Timestamp: time.Now(),
		Text:      "kernel update",
		Tags:      []string{"kernel"},
		Resources: []cache.AnnotationResource{{Type: cache.EventNode, ID: "node1", Name: "worker"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := auth.ContextWithIdentity(context.Background(), &auth.Identity{Subject: "bob"})

	body, err := srv.readResource(
		ctx,
		"cetacean://annotations/"+strconv.FormatUint(onService.ID, 10),
	)
	if err != nil {
		t.Fatal(err)
	}

	var got annotationResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Text != "load test" || got.Version != onService.Version {
		t.Errorf("annotation = %+v", got)
	}

	if _, err := srv.readResource(
		ctx, "cetacean://annotations/"+strconv.FormatUint(onNode.ID, 10),
	); err == nil {
		t.Error("bob reads the note on the node he may not read")
	}

	tags, err := srv.readResource(ctx, "cetacean://annotations/tags")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tags, `"load"`) || strings.Contains(tags, "kernel") {
		t.Errorf("tags = %s, want the service note's alone", tags)
	}
}

func TestAnnotationSubscriptionsHearOfEditsTheReaderMaySee(t *testing.T) {
	srv, c := annotationServer(t, true)
	note := alicesNote(t, c)
	hidden, err := c.CreateAnnotation(context.Background(), cache.Annotation{
		Timestamp: time.Now(),
		Text:      "kernel update",
		Resources: []cache.AnnotationResource{{Type: cache.EventNode, ID: "node1", Name: "worker"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	bob := &auth.Identity{Subject: "bob"}
	name := t.Name()
	uri := "cetacean://annotations/" + strconv.FormatUint(note.ID, 10)
	srv.notifications.Subscribe(session(name), uri, bob)
	srv.notifications.Subscribe(
		session(name), "cetacean://annotations/"+strconv.FormatUint(hidden.ID, 10), bob,
	)

	for _, id := range []uint64{note.ID, hidden.ID} {
		if _, err := c.UpdateAnnotation(context.Background(), id, func(a *cache.Annotation) error {
			a.Text += ", edited"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertUpdated(t, drainUpdated(t, name), uri)

	if err := c.DeleteAnnotation(context.Background(), note.ID, func(cache.Annotation) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertUpdated(t, drainUpdated(t, name), uri)
}

func TestGetEventsFiltersAnnotations(t *testing.T) {
	srv, c := annotationServer(t, true)
	both := alicesNote(t, c, "deploy", "web")
	alicesNote(t, c, "deploy")

	ctx := auth.ContextWithIdentity(context.Background(), &auth.Identity{Subject: "bob"})

	read := func(args map[string]any) []string {
		t.Helper()

		body, err := srv.toolGetEvents(ctx, toolRequest(t, "get_events", args))
		if err != nil {
			t.Fatal(err)
		}

		var got eventsResult
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatal(err)
		}

		var ids []string
		for _, e := range got.Entries {
			if e.Kind == cache.KindAnnotation {
				if e.ID == 0 || e.Version == 0 {
					t.Errorf("entry %+v lacks its id or version", e)
				}
				ids = append(ids, strconv.FormatUint(e.ID, 10))
			}
		}

		return ids
	}

	if got := read(map[string]any{"tags": []any{"deploy", "web"}}); len(got) != 1 ||
		got[0] != strconv.FormatUint(both.ID, 10) {
		t.Errorf("every tag = %v, want the one carrying both", got)
	}
	if got := read(
		map[string]any{"tags": []any{"deploy", "web"}, "matchAny": true},
	); len(
		got,
	) != 2 {
		t.Errorf("any tag = %v, want both notes", got)
	}
	if got := read(map[string]any{"authors": []any{"carol"}}); len(got) != 0 {
		t.Errorf("carol's = %v, want none", got)
	}
	if got := read(map[string]any{"kinds": []any{"change"}}); len(got) != 0 {
		t.Errorf("changes alone = %v, want no notes", got)
	}
	if got := read(map[string]any{"search": "LOAD alice"}); len(got) != 2 {
		t.Errorf("a search every note's text and author hold = %v, want both", got)
	}
	if got := read(map[string]any{"search": "load rollback"}); len(got) != 0 {
		t.Errorf("a search with a word no note holds = %v, want none", got)
	}

	if _, err := srv.toolGetEvents(
		ctx, toolRequest(t, "get_events", map[string]any{"kinds": []any{"changes"}}),
	); err == nil || !strings.Contains(err.Error(), `"changes"`) {
		t.Errorf("an unknown kind = %v, want it refused by name", err)
	}
}
