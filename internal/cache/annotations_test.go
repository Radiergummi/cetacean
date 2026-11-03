package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeAnnotationStore records what was committed and fails while err is set.
type fakeAnnotationStore struct {
	err     error
	put     []Annotation
	deleted []uint64
}

func (f *fakeAnnotationStore) PutAnnotation(_ context.Context, a Annotation) error {
	if f.err != nil {
		return f.err
	}

	f.put = append(f.put, a)

	return nil
}

func (f *fakeAnnotationStore) DeleteAnnotation(_ context.Context, id uint64) error {
	if f.err != nil {
		return f.err
	}

	f.deleted = append(f.deleted, id)

	return nil
}

func annotatedCache(t *testing.T) (*Cache, *fakeAnnotationStore) {
	t.Helper()

	c := New(nil)
	store := &fakeAnnotationStore{}
	c.RestoreAnnotations(store, nil)

	return c, store
}

func TestCreateAnnotation_TakesTheNextSequenceID(t *testing.T) {
	c, store := annotatedCache(t)
	c.history.Append(HistoryEntry{Type: EventService, ResourceID: "svc1"})

	a, err := c.CreateAnnotation(context.Background(), Annotation{
		Timestamp: time.Now(),
		Text:      "load test started",
		Tags:      []string{"loadtest"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if a.ID != 2 || a.Version != 2 || a.Created.IsZero() {
		t.Fatalf("created %+v, want ID and version 2 and a creation time", a)
	}

	if len(store.put) != 1 || store.put[0].ID != 2 {
		t.Fatalf("committed %+v, want the annotation 2", store.put)
	}

	if got, ok := c.Annotation(2); !ok || got.Text != a.Text {
		t.Fatalf("index holds %+v, ok=%v", got, ok)
	}

	items, _ := c.history.Since(1)
	if len(items) != 1 || items[0].ID != 2 || items[0].Kind != KindAnnotation ||
		items[0].Action != "create" || items[0].ResourceID != "2" {
		t.Fatalf("ring items %+v, want one annotation create at 2", items)
	}
}

func TestAnnotationItems_AreNeverListed(t *testing.T) {
	c, _ := annotatedCache(t)

	a, err := c.CreateAnnotation(context.Background(), Annotation{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.UpdateAnnotation(context.Background(), a.ID, func(*Annotation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}

	for _, q := range []HistoryQuery{{}, {Kinds: []string{KindAnnotation}}, {ResourceID: "1"}} {
		if got := c.history.List(q); len(got) != 0 {
			t.Errorf("List(%+v) = %+v, want nothing", q, got)
		}
	}
}

func TestAnnotationWrite_FailedCommitRecordsNothing(t *testing.T) {
	c, store := annotatedCache(t)
	store.err = errors.New("disk full")

	if _, err := c.CreateAnnotation(context.Background(), Annotation{Text: "x"}); err == nil {
		t.Fatal("create succeeded on a failing store")
	}

	if c.history.Count() != 0 || c.AnnotationCount() != 0 {
		t.Fatalf("count %d, %d indexed; want nothing", c.history.Count(), c.AnnotationCount())
	}
}

func TestAnnotationWrite_NeedsAStore(t *testing.T) {
	c := New(nil)

	if _, err := c.CreateAnnotation(
		context.Background(),
		Annotation{Text: "x"},
	); !errors.Is(err, ErrAnnotationsOff) {
		t.Fatalf("err = %v, want ErrAnnotationsOff", err)
	}

	if c.history.Count() != 0 {
		t.Fatalf("count %d, want nothing appended", c.history.Count())
	}
}

func TestCreateAnnotation_RefusesPastTheCap(t *testing.T) {
	c := New(nil)

	existing := make([]Annotation, MaxAnnotations)
	for i := range existing {
		existing[i].ID = uint64(i + 1)
	}

	c.RestoreAnnotations(&fakeAnnotationStore{}, existing)

	if _, err := c.CreateAnnotation(
		context.Background(),
		Annotation{Text: "x"},
	); !errors.Is(err, ErrAnnotationLimit) {
		t.Fatalf("err = %v, want ErrAnnotationLimit", err)
	}
}

func TestUpdateAnnotation_KeepsIdentity(t *testing.T) {
	c, store := annotatedCache(t)
	author := &Actor{Subject: "alice", Provider: "oidc"}
	editor := &Actor{Subject: "bob", Provider: "oidc"}

	created, err := c.CreateAnnotation(
		context.Background(),
		Annotation{Text: "x", Tags: []string{"a"}, Actor: author},
	)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := c.UpdateAnnotation(context.Background(), created.ID, func(a *Annotation) error {
		a.ID, a.Actor, a.Text, a.Editor = 99, editor, "y", editor
		a.Tags[0] = "b"

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if updated.ID != created.ID || updated.Actor != author || updated.Editor != editor ||
		updated.Version != 2 || updated.Updated.IsZero() || updated.Text != "y" {
		t.Fatalf("updated %+v", updated)
	}

	if store.put[0].Tags[0] != "a" {
		t.Fatal("the edit changed the stored original's tags")
	}

	if got, _ := c.Annotation(created.ID); got.Version != 2 {
		t.Fatalf("index holds version %d, want 2", got.Version)
	}
}

func TestUpdateAnnotation_RefusedEditRecordsNothing(t *testing.T) {
	c, _ := annotatedCache(t)

	a, err := c.CreateAnnotation(context.Background(), Annotation{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}

	refused := errors.New("precondition failed")

	_, err = c.UpdateAnnotation(context.Background(), a.ID, func(*Annotation) error {
		return refused
	})
	if !errors.Is(err, refused) || c.history.Count() != 1 {
		t.Fatalf(
			"err = %v, count %d; want the refusal and nothing appended",
			err,
			c.history.Count(),
		)
	}
}

func TestDeleteAnnotation(t *testing.T) {
	c, store := annotatedCache(t)

	a, err := c.CreateAnnotation(context.Background(), Annotation{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}

	if err := c.DeleteAnnotation(
		context.Background(),
		a.ID,
		func(Annotation) error { return nil },
	); err != nil {
		t.Fatal(err)
	}

	if _, ok := c.Annotation(a.ID); ok || len(store.deleted) != 1 {
		t.Fatalf("annotation still indexed, or %v deleted", store.deleted)
	}

	items, _ := c.history.Since(1)
	if len(items) != 1 || items[0].Action != "remove" || items[0].ResourceID != "1" {
		t.Fatalf("ring items %+v, want one remove of 1", items)
	}

	if err := c.DeleteAnnotation(
		context.Background(),
		a.ID,
		func(Annotation) error { return nil },
	); !errors.Is(err, ErrAnnotationNotFound) {
		t.Fatalf("second delete: %v, want ErrAnnotationNotFound", err)
	}
}
