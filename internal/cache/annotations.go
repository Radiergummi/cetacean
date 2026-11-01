package cache

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
	"time"
)

// MaxAnnotations is how many live annotations there may be; the next one is
// refused rather than evicting someone's note.
const MaxAnnotations = 10_000

var (
	ErrAnnotationsOff     = errors.New("annotations need storage.timeline")
	ErrAnnotationLimit    = errors.New("the annotation limit is reached")
	ErrAnnotationNotFound = errors.New("annotation not found")
)

// Annotation is a note about a moment or a range, written by a person or an
// agent. Its ID is the sequence ID of the item that created it.
type Annotation struct {
	ID        uint64               `json:"id"`
	Timestamp time.Time            `json:"timestamp"`
	Until     time.Time            `json:"until,omitzero"`
	Text      string               `json:"text"`
	Tags      []string             `json:"tags"`
	Resources []AnnotationResource `json:"resources"`
	Actor     *Actor               `json:"actor,omitempty"`
	Updated   time.Time            `json:"updated,omitzero"`
	Editor    *Actor               `json:"editor,omitempty"`

	// Version is the sequence ID of the last write.
	Version uint64    `json:"-"`
	Created time.Time `json:"-"`
}

// AnnotationResource is a resource an annotation names. Service and Stack are
// kept from creation, so the annotation can be authorized once it is gone.
type AnnotationResource struct {
	Type    EventType `json:"type"`
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Service string    `json:"-"`
	Stack   string    `json:"-"`
}

// AnnotationStore persists annotations; a write is committed before it returns.
type AnnotationStore interface {
	PutAnnotation(ctx context.Context, a Annotation) error
	DeleteAnnotation(ctx context.Context, id uint64) error
}

type annotationIndex struct {
	mu    sync.RWMutex
	store AnnotationStore
	byID  map[uint64]Annotation
}

// RestoreAnnotations loads the stored annotations and makes store the one
// writes go to; until it is called, every write fails with ErrAnnotationsOff.
func (c *Cache) RestoreAnnotations(store AnnotationStore, annotations []Annotation) {
	c.annotations.mu.Lock()
	defer c.annotations.mu.Unlock()

	c.annotations.store = store
	for _, a := range annotations {
		c.annotations.byID[a.ID] = a
	}
}

// Annotation returns one annotation. Its slices are shared and must not be
// modified.
func (c *Cache) Annotation(id uint64) (Annotation, bool) {
	c.annotations.mu.RLock()
	defer c.annotations.mu.RUnlock()

	a, ok := c.annotations.byID[id]

	return a, ok
}

// CreateAnnotation records a new annotation, assigning its ID, version and
// creation time.
func (c *Cache) CreateAnnotation(ctx context.Context, a Annotation) (Annotation, error) {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()

	if c.annotationCount() >= MaxAnnotations {
		return Annotation{}, ErrAnnotationLimit
	}

	a.ID = c.history.Count() + 1
	a.Version = a.ID
	a.Created = time.Now()
	a.Tags = slices.Clone(a.Tags)
	a.Resources = slices.Clone(a.Resources)
	a.Updated, a.Editor = time.Time{}, nil

	return a, c.commitAnnotation(ctx, a, "create")
}

// UpdateAnnotation applies edit to a copy of the annotation and records the
// result; an error from edit records nothing. edit runs under the lock every
// event waits on and must not call back into the cache. It sets the editor;
// the ID, author and creation time are kept.
func (c *Cache) UpdateAnnotation(
	ctx context.Context,
	id uint64,
	edit func(*Annotation) error,
) (Annotation, error) {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()

	current, ok := c.Annotation(id)
	if !ok {
		return Annotation{}, ErrAnnotationNotFound
	}

	a := current
	a.Tags = slices.Clone(a.Tags)
	a.Resources = slices.Clone(a.Resources)

	if err := edit(&a); err != nil {
		return Annotation{}, err
	}

	a.ID, a.Actor, a.Created = current.ID, current.Actor, current.Created
	a.Version = c.history.Count() + 1
	a.Updated = time.Now()

	return a, c.commitAnnotation(ctx, a, "update")
}

// DeleteAnnotation removes an annotation once check accepts it; check runs
// as edit does in UpdateAnnotation.
func (c *Cache) DeleteAnnotation(
	ctx context.Context,
	id uint64,
	check func(Annotation) error,
) error {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()

	current, ok := c.Annotation(id)
	if !ok {
		return ErrAnnotationNotFound
	}

	if err := check(current); err != nil {
		return err
	}

	return c.commitAnnotation(ctx, Annotation{ID: id}, "remove")
}

func (c *Cache) annotationCount() int {
	c.annotations.mu.RLock()
	defer c.annotations.mu.RUnlock()

	return len(c.annotations.byID)
}

// commitAnnotation commits a write, then indexes it and appends its item, all
// under notifyMu: every Append runs under it, so the ID taken from Count is
// the one the item gets. The index changes first, so a validator read from
// Count never names content the index does not hold yet.
func (c *Cache) commitAnnotation(ctx context.Context, a Annotation, action string) error {
	c.annotations.mu.RLock()
	store := c.annotations.store
	c.annotations.mu.RUnlock()

	if store == nil {
		return ErrAnnotationsOff
	}

	// A cancelled transaction discards the store's one connection, its lock with it.
	ctx = context.WithoutCancel(ctx)

	var err error
	if action == "remove" {
		err = store.DeleteAnnotation(ctx, a.ID)
	} else {
		err = store.PutAnnotation(ctx, a)
	}

	if err != nil {
		return err
	}

	c.annotations.mu.Lock()
	if action == "remove" {
		delete(c.annotations.byID, a.ID)
	} else {
		c.annotations.byID[a.ID] = a
	}
	c.annotations.mu.Unlock()

	c.history.Append(HistoryEntry{
		Kind:       KindAnnotation,
		Action:     action,
		ResourceID: strconv.FormatUint(a.ID, 10),
	})

	return nil
}
