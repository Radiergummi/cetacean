package cluster_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

type memoryAnnotationStore struct{}

func (memoryAnnotationStore) PutAnnotation(context.Context, cache.Annotation) error { return nil }
func (memoryAnnotationStore) DeleteAnnotation(context.Context, uint64) error        { return nil }

// annotationCache holds service "web" in stack "shop" and node "node1".
func annotationCache(t *testing.T) *cache.Cache {
	t.Helper()

	c := newTestCache()
	c.SetService(swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
		Name:   "web",
		Labels: map[string]string{cache.StackLabel: "shop"},
	}}})
	c.SetNode(swarm.Node{ID: "node1", Description: swarm.NodeDescription{Hostname: "worker"}})
	c.RestoreAnnotations(memoryAnnotationStore{}, nil)

	return c
}

func grants(resources ...string) func(string) bool {
	return func(resource string) bool { return slices.Contains(resources, resource) }
}

func as(subject string) context.Context {
	return cache.WithActor(context.Background(), cache.Actor{
		Subject: subject, Provider: "oidc", Transport: cache.TransportREST,
	})
}

var (
	everyone = cluster.AnnotationCaller{
		CanRead: grants("service:web", "stack:shop", "node:worker"), CanWrite: grants(
			"service:web", "stack:shop", "node:worker",
		),
		AnyGrant: true, WritesEverything: true,
	}
	reader = cluster.AnnotationCaller{
		CanRead: grants("service:web", "stack:shop"), CanWrite: grants(), AnyGrant: true,
	}
)

func onWeb(text string) cluster.AnnotationInput {
	return cluster.AnnotationInput{
		Text:      text,
		Resources: []cluster.AnnotationRef{{Type: "service", ID: "web"}},
	}
}

func TestCreateAnnotationRecordsTheResolvedResourceAndAuthor(t *testing.T) {
	s := cluster.NewAnnotations(annotationCache(t))

	a, err := s.Create(as("alice"), everyone, onWeb("load test started"))
	if err != nil {
		t.Fatal(err)
	}

	want := cache.AnnotationResource{
		Type: cache.EventService, ID: "svc1", Name: "web", Service: "web", Stack: "shop",
	}
	if len(a.Resources) != 1 || a.Resources[0] != want {
		t.Errorf("resources = %+v, want %+v", a.Resources, want)
	}
	if a.Actor == nil || a.Actor.Subject != "alice" {
		t.Errorf("actor = %+v, want alice", a.Actor)
	}
	if a.Timestamp.IsZero() || a.ID == 0 || a.Version != a.ID {
		t.Errorf("annotation = %+v, want a timestamp and its ID as its version", a)
	}
}

func TestCreateAnnotationRefusesAnInvalidBody(t *testing.T) {
	s := cluster.NewAnnotations(annotationCache(t))
	now := time.Now()

	for name, tc := range map[string]struct {
		in    cluster.AnnotationInput
		field string
	}{
		"no text":        {cluster.AnnotationInput{Text: "  "}, "text"},
		"long text":      {cluster.AnnotationInput{Text: strings.Repeat("é", 1001)}, "text"},
		"until first":    {cluster.AnnotationInput{Text: "x", Timestamp: now, Until: now}, "until"},
		"bad tag":        {cluster.AnnotationInput{Text: "x", Tags: []string{"Deploy"}}, "tags[0]"},
		"tag twice":      {cluster.AnnotationInput{Text: "x", Tags: []string{"a", "a"}}, "tags[1]"},
		"too many tags":  {cluster.AnnotationInput{Text: "x", Tags: make([]string, 11)}, "tags"},
		"unknown type":   {refs("x", cluster.AnnotationRef{Type: "task", ID: "t1"}), "resources[0].type"},
		"missing":        {refs("x", cluster.AnnotationRef{Type: "service", ID: "api"}), "resources[0].id"},
		"no id":          {refs("x", cluster.AnnotationRef{Type: "node"}), "resources[0].id"},
		"named twice":    {refs("x", ref("service", "web"), ref("service", "svc1")), "resources[1]"},
		"too many named": {refs("x", make([]cluster.AnnotationRef, 21)...), "resources"},
	} {
		_, err := s.Create(as("alice"), everyone, tc.in)

		invalid, ok := errors.AsType[*cluster.AnnotationInvalidError](err)
		if !ok || invalid.Field != tc.field {
			t.Errorf("%s: err = %v, want one naming %s", name, err, tc.field)
		}
	}
}

func ref(typ, id string) cluster.AnnotationRef { return cluster.AnnotationRef{Type: typ, ID: id} }

func refs(text string, r ...cluster.AnnotationRef) cluster.AnnotationInput {
	return cluster.AnnotationInput{Text: text, Resources: r}
}

func TestCreateAnnotationNeedsWriteOnEveryResource(t *testing.T) {
	s := cluster.NewAnnotations(annotationCache(t))
	webOnly := cluster.AnnotationCaller{
		CanRead: grants("service:web", "node:worker"), CanWrite: grants("service:web"),
	}

	if _, err := s.Create(as("bob"), webOnly, onWeb("ok")); err != nil {
		t.Errorf("write on the service: err = %v, want none", err)
	}

	both := refs("x", ref("service", "web"), ref("node", "node1"))
	if _, err := s.Create(as("bob"), webOnly, both); !errors.Is(err, cluster.ErrAnnotationDenied) {
		t.Errorf("no write on the node: err = %v, want denied", err)
	}

	if _, err := s.Create(as("bob"), webOnly, refs("x")); !errors.Is(
		err,
		cluster.ErrAnnotationDenied,
	) {
		t.Errorf("cluster-wide without write on *: err = %v, want denied", err)
	}
	if _, err := s.Create(as("bob"), everyone, refs("x")); err != nil {
		t.Errorf("cluster-wide with write on *: err = %v, want none", err)
	}
}

func TestGetAnnotationNeedsReadOnEveryResource(t *testing.T) {
	s := cluster.NewAnnotations(annotationCache(t))

	a, err := s.Create(
		as("alice"),
		everyone,
		refs("x", ref("service", "web"), ref("node", "node1")),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Get(reader, a.ID); !errors.Is(err, cache.ErrAnnotationNotFound) {
		t.Errorf("one resource unreadable: err = %v, want not found", err)
	}
	if _, err := s.Get(everyone, a.ID); err != nil {
		t.Errorf("all readable: err = %v", err)
	}

	wide, err := s.Create(as("alice"), everyone, refs("cluster-wide"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(reader, wide.ID); err != nil {
		t.Errorf("cluster-wide with a grant: err = %v", err)
	}
	if _, err := s.Get(cluster.AnnotationCaller{CanRead: grants()}, wide.ID); err == nil {
		t.Error("cluster-wide without any grant was readable")
	}
}

func TestAnAnnotationOnARemovedServiceIsReadByItsStack(t *testing.T) {
	c := annotationCache(t)
	s := cluster.NewAnnotations(c)

	a, err := s.Create(as("alice"), everyone, onWeb("before removal"))
	if err != nil {
		t.Fatal(err)
	}
	c.DeleteService("svc1")

	stackReader := cluster.AnnotationCaller{CanRead: grants("stack:shop"), CanWrite: grants()}
	if _, err := s.Get(stackReader, a.ID); err != nil {
		t.Errorf("read through the recorded stack: err = %v", err)
	}
}

func TestTheAuthorEditsWithReadAloneAndOthersNeedWrite(t *testing.T) {
	s := cluster.NewAnnotations(annotationCache(t))

	a, err := s.Create(as("alice"), everyone, onWeb("first"))
	if err != nil {
		t.Fatal(err)
	}

	text := "second"
	edited, err := s.Update(as("alice"), reader, a.ID, cluster.AnnotationPatch{Text: &text}, nil)
	if err != nil {
		t.Fatalf("author with read: err = %v", err)
	}
	if edited.Text != "second" || edited.Editor == nil || edited.Editor.Subject != "alice" ||
		edited.Version <= a.Version || edited.Actor.Subject != "alice" {
		t.Errorf("edited = %+v, want the text, alice as editor and a newer version", edited)
	}

	_, err = s.Update(as("bob"), reader, a.ID, cluster.AnnotationPatch{Text: &text}, nil)
	if !errors.Is(err, cluster.ErrAnnotationDenied) {
		t.Errorf("another reader: err = %v, want denied", err)
	}

	if err := s.Delete(as("bob"), reader, a.ID, nil); !errors.Is(err, cluster.ErrAnnotationDenied) {
		t.Errorf("another reader deleting: err = %v, want denied", err)
	}
	if err := s.Delete(as("bob"), everyone, a.ID, nil); err != nil {
		t.Errorf("a writer deleting: err = %v", err)
	}
}

func TestAnEditNamingNewResourcesNeedsWriteOnThem(t *testing.T) {
	s := cluster.NewAnnotations(annotationCache(t))

	a, err := s.Create(as("alice"), everyone, onWeb("x"))
	if err != nil {
		t.Fatal(err)
	}

	moved := []cluster.AnnotationRef{ref("service", "web"), ref("node", "worker")}
	patch := cluster.AnnotationPatch{Resources: &moved}
	if _, err := s.Update(as("alice"), reader, a.ID, patch, nil); !errors.Is(
		err, cluster.ErrAnnotationDenied,
	) {
		t.Errorf("author adding a node they cannot write: err = %v, want denied", err)
	}

	edited, err := s.Update(as("alice"), everyone, a.ID, patch, nil)
	if err != nil || len(edited.Resources) != 2 || edited.Resources[1].ID != "node1" {
		t.Errorf("edited = %+v, %v; want the node added by its ID", edited.Resources, err)
	}
}

func TestAnEditKeepsARemovedResource(t *testing.T) {
	c := annotationCache(t)
	s := cluster.NewAnnotations(c)

	a, err := s.Create(as("alice"), everyone, onWeb("x"))
	if err != nil {
		t.Fatal(err)
	}
	c.DeleteService("svc1")

	kept := []cluster.AnnotationRef{ref("service", "svc1")}
	edited, err := s.Update(
		as("alice"),
		everyone,
		a.ID,
		cluster.AnnotationPatch{Resources: &kept},
		nil,
	)
	if err != nil || len(edited.Resources) != 1 || edited.Resources[0] != a.Resources[0] {
		t.Errorf("edited = %+v, %v; want the recorded service kept", edited.Resources, err)
	}
}

func TestAnEditHonoursItsPrecondition(t *testing.T) {
	s := cluster.NewAnnotations(annotationCache(t))

	a, err := s.Create(as("alice"), everyone, onWeb("x"))
	if err != nil {
		t.Fatal(err)
	}

	text := "y"
	stale := func(current cache.Annotation) bool { return current.Version == a.Version-1 }
	_, err = s.Update(as("alice"), everyone, a.ID, cluster.AnnotationPatch{Text: &text}, stale)
	if !errors.Is(err, cluster.ErrAnnotationPrecondition) {
		t.Errorf("stale version: err = %v, want the precondition failing", err)
	}

	if err := s.Delete(as("alice"), everyone, a.ID, stale); !errors.Is(
		err, cluster.ErrAnnotationPrecondition,
	) {
		t.Errorf("stale delete: err = %v, want the precondition failing", err)
	}
}

func TestAnUntilClearedByAPatchMakesAPoint(t *testing.T) {
	s := cluster.NewAnnotations(annotationCache(t))
	now := time.Now()

	in := onWeb("window")
	in.Timestamp, in.Until = now, now.Add(time.Hour)
	a, err := s.Create(as("alice"), everyone, in)
	if err != nil {
		t.Fatal(err)
	}

	edited, err := s.Update(as("alice"), everyone, a.ID, cluster.AnnotationPatch{
		Until: new(time.Time{}),
	}, nil)
	if err != nil || !edited.Until.IsZero() {
		t.Errorf("edited until = %v, %v; want it cleared", edited.Until, err)
	}
}

func TestAnnotationWritesNeedTheTimeline(t *testing.T) {
	s := cluster.NewAnnotations(newTestCache())
	text := "x"

	if _, err := s.Create(as("alice"), everyone, refs("x")); !errors.Is(
		err,
		cache.ErrAnnotationsOff,
	) {
		t.Errorf("create: err = %v, want annotations off", err)
	}
	if _, err := s.Update(
		as("alice"),
		everyone,
		1,
		cluster.AnnotationPatch{Text: &text},
		nil,
	); !errors.Is(
		err,
		cache.ErrAnnotationsOff,
	) {
		t.Errorf("update: err = %v, want annotations off", err)
	}
	if err := s.Delete(as("alice"), everyone, 1, nil); !errors.Is(err, cache.ErrAnnotationsOff) {
		t.Errorf("delete: err = %v, want annotations off", err)
	}
}
