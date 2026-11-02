package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

type discardedAnnotations struct{}

func (discardedAnnotations) PutAnnotation(context.Context, cache.Annotation) error { return nil }
func (discardedAnnotations) DeleteAnnotation(context.Context, uint64) error        { return nil }

// annotationServer serves service "web" and node "worker": alice writes the
// service, bob only reads it, and neither may read the node.
func annotationServer(t *testing.T, timeline bool) (*Server, *cache.Cache) {
	t.Helper()

	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
		Name: "web", Labels: map[string]string{cache.StackLabel: "shop"},
	}}})
	c.SetNode(swarm.Node{ID: "node1", Description: swarm.NodeDescription{Hostname: "worker"}})
	if timeline {
		c.RestoreAnnotations(discardedAnnotations{}, nil)
	}

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:web"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"write"},
		},
		{
			Resources:   []string{"service:web"},
			Audience:    []string{"user:bob"},
			Permissions: []string{"read"},
		},
	}})

	return newResourceTestServer(t, c, func(o *Options) { o.ACL = e }), c
}

func annotatingAs(subject string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := &auth.Identity{Subject: subject, DisplayName: subject, Provider: "headers"}
		handler.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), identity)))
	})
}

func TestAddAnnotationRecordsAnAttributedNote(t *testing.T) {
	srv, c := annotationServer(t, true)

	result := callTool(
		t,
		annotatingAs("alice", srv.Handler()),
		`{"name":"add_annotation","arguments":{
		"text":"load test","at":"2026-11-01T14:00:00Z","until":"2026-11-01T14:30:00Z",
		"tags":["load-test"],"resources":[{"type":"service","id":"web"}]}}`,
	)

	var got annotationResult
	if err := json.Unmarshal(result.StructuredContent, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, result.StructuredContent)
	}
	if got.Text != "load test" || got.At != "2026-11-01T14:00:00.000000000Z" ||
		len(got.Resources) != 1 || got.Resources[0].ID != "svc1" {
		t.Errorf("result = %+v", got)
	}

	stored, ok := c.Annotation(got.ID)
	if !ok || stored.Actor == nil || stored.Actor.Subject != "alice" ||
		stored.Actor.Transport != cache.TransportMCP {
		t.Errorf("stored = %+v, want alice via MCP as its author", stored)
	}
}

func TestAddAnnotationRefusals(t *testing.T) {
	for _, tt := range []struct {
		name, subject, args, want string
		timeline                  bool
	}{
		{
			"a reader",
			"bob",
			`{"text":"x","resources":[{"type":"service","id":"web"}]}`,
			"write access denied",
			true,
		},
		{"cluster-wide without write on everything", "alice", `{"text":"x"}`, "write access denied", true},
		{
			"a resource the caller cannot read",
			"alice",
			`{"text":"x","resources":[{"type":"node","id":"worker"}]}`,
			"not found",
			true,
		},
		{"a malformed time", "alice", `{"text":"x","at":"yesterday"}`, "at: must be an RFC 3339", true},
		{
			"tags that are not a list of strings",
			"alice",
			`{"text":"x","tags":"load-test","resources":[{"type":"service","id":"web"}]}`,
			"tags: must be a list of strings",
			true,
		},
		{
			"no timeline",
			"alice",
			`{"text":"x","resources":[{"type":"service","id":"web"}]}`,
			"storage.timeline",
			false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := annotationServer(t, tt.timeline)

			raw := callToolExpectingError(t, annotatingAs(tt.subject, srv.Handler()),
				`{"name":"add_annotation","arguments":`+tt.args+`}`)
			if !strings.Contains(raw, tt.want) {
				t.Errorf("error = %s, want it to mention %q", raw, tt.want)
			}
		})
	}
}

func TestGetEventsListsTheAnnotationsTheCallerMayRead(t *testing.T) {
	srv, c := annotationServer(t, true)
	at := time.Date(2026, 11, 1, 14, 0, 0, 0, time.UTC)

	for _, a := range []cache.Annotation{
		{
			Timestamp: at,
			Text:      "load test",
			Tags:      []string{"load-test"},
			Resources: []cache.AnnotationResource{{Type: cache.EventService, ID: "svc1", Name: "web"}},
		},
		{
			Timestamp: at,
			Text:      "kernel update",
			Resources: []cache.AnnotationResource{{Type: cache.EventNode, ID: "node1", Name: "worker"}},
		},
	} {
		if _, err := c.CreateAnnotation(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}

	ctx := auth.ContextWithIdentity(context.Background(), &auth.Identity{Subject: "bob"})
	body, err := srv.toolGetEvents(
		ctx,
		toolRequest(t, "get_events", map[string]any{}),
	)
	if err != nil {
		t.Fatal(err)
	}

	var got eventsResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}

	var notes []string
	for _, e := range got.Entries {
		if e.Kind == cache.KindAnnotation {
			notes = append(notes, e.Text)
			if e.At != "2026-11-01T14:00:00.000000000Z" || len(e.Tags) != 1 ||
				len(e.Resources) != 1 || e.Resources[0].Name != "web" {
				t.Errorf("entry = %+v", e)
			}
		}
	}
	if len(notes) != 1 || notes[0] != "load test" {
		t.Errorf("bob reads annotations %q, want only the one on web", notes)
	}
}

func TestGetEventsCutsItsLimitByTimeNotByCreation(t *testing.T) {
	srv, c := annotationServer(t, true)
	now := time.Now().UTC()

	if _, err := c.CreateAnnotation(context.Background(), cache.Annotation{
		Timestamp: now.Add(-72 * time.Hour),
		Text:      "three days ago",
		Resources: []cache.AnnotationResource{{Type: cache.EventService, ID: "svc1", Name: "web"}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Duration{-2 * time.Minute, -time.Minute} {
		c.History().Append(cache.HistoryEntry{
			Timestamp: now.Add(at), Type: cache.EventService, Action: "update",
			ResourceID: "svc1", Name: "web",
		})
	}

	ctx := auth.ContextWithIdentity(context.Background(), &auth.Identity{Subject: "bob"})
	body, err := srv.toolGetEvents(ctx, toolRequest(t, "get_events", map[string]any{"limit": 2}))
	if err != nil {
		t.Fatal(err)
	}

	var got eventsResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	for _, e := range got.Entries {
		if e.Kind == cache.KindAnnotation {
			t.Errorf("the newest two by time are the changes, got %+v", got.Entries)
		}
	}
}

func TestHistoryResourceListsAnnotations(t *testing.T) {
	srv, c := annotationServer(t, true)
	if _, err := c.CreateAnnotation(context.Background(), cache.Annotation{
		Timestamp: time.Now(),
		Text:      "load test",
		Resources: []cache.AnnotationResource{{Type: cache.EventService, ID: "svc1", Name: "web"}},
	}); err != nil {
		t.Fatal(err)
	}

	ctx := auth.ContextWithIdentity(context.Background(), &auth.Identity{Subject: "bob"})
	body, err := srv.readResource(ctx, "cetacean://history")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "load test") {
		t.Errorf("cetacean://history leaves out the annotation get_events lists: %s", body)
	}
}
