package cache

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
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

	// Removed is set for a response by WithRemovals; it is never stored.
	Removed bool `json:"removed,omitempty"`
}

// AnnotationStore persists annotations.
type AnnotationStore interface {
	// BeginAnnotation waits, as long as ctx allows, until the store can take a
	// write, and holds it for one, so that write never waits behind another.
	BeginAnnotation(ctx context.Context) (AnnotationWrite, error)
}

// AnnotationWrite is a store held for one write. Put and Delete commit before
// they return, whatever ctx does; Release frees the store, written or not.
type AnnotationWrite interface {
	Put(ctx context.Context, a Annotation) error
	Delete(ctx context.Context, id uint64) error
	Release()
}

// annotationIndex holds the annotations ordered by ID. IDs only grow, so a
// create appends and a list binary-searches to its cursor.
type annotationIndex struct {
	mu      sync.RWMutex
	store   AnnotationStore
	ordered []Annotation
}

// find is where the annotation with id is, or would be inserted. The caller
// holds mu.
func (x *annotationIndex) find(id uint64) (int, bool) {
	return slices.BinarySearchFunc(x.ordered, id, func(a Annotation, id uint64) int {
		return cmp.Compare(a.ID, id)
	})
}

// RestoreAnnotations loads the stored annotations and makes store the one
// writes go to; until it is called, every write fails with ErrAnnotationsOff.
func (c *Cache) RestoreAnnotations(store AnnotationStore, annotations []Annotation) {
	c.annotations.mu.Lock()
	defer c.annotations.mu.Unlock()

	c.annotations.store = store
	c.annotations.ordered = append(c.annotations.ordered, annotations...)
	slices.SortFunc(
		c.annotations.ordered,
		func(a, b Annotation) int { return cmp.Compare(a.ID, b.ID) },
	)
}

// Annotation returns one annotation. Its slices are shared and must not be
// modified.
func (c *Cache) Annotation(id uint64) (Annotation, bool) {
	c.annotations.mu.RLock()
	defer c.annotations.mu.RUnlock()

	i, ok := c.annotations.find(id)
	if !ok {
		return Annotation{}, false
	}

	return c.annotations.ordered[i], true
}

// CreateAnnotation records a new annotation, assigning its ID, version and
// creation time.
func (c *Cache) CreateAnnotation(ctx context.Context, a Annotation) (Annotation, error) {
	write, err := c.beginAnnotation(ctx)
	if err != nil {
		return Annotation{}, err
	}
	defer write.Release()

	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()

	if c.AnnotationCount() >= MaxAnnotations {
		return Annotation{}, ErrAnnotationLimit
	}

	a.ID = c.history.Count() + 1
	a.Version = a.ID
	a.Created = time.Now().UTC()
	a.Tags = slices.Clone(a.Tags)
	a.Resources = slices.Clone(a.Resources)
	a.Updated, a.Editor = time.Time{}, nil

	return a, c.commitAnnotation(ctx, write, a, "create")
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
	write, err := c.beginAnnotation(ctx)
	if err != nil {
		return Annotation{}, err
	}
	defer write.Release()

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
	a.Updated = time.Now().UTC()

	return a, c.commitAnnotation(ctx, write, a, "update")
}

// DeleteAnnotation removes an annotation once check accepts it; check runs
// as edit does in UpdateAnnotation.
func (c *Cache) DeleteAnnotation(
	ctx context.Context,
	id uint64,
	check func(Annotation) error,
) error {
	write, err := c.beginAnnotation(ctx)
	if err != nil {
		return err
	}
	defer write.Release()

	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()

	current, ok := c.Annotation(id)
	if !ok {
		return ErrAnnotationNotFound
	}

	if err := check(current); err != nil {
		return err
	}

	return c.commitAnnotation(ctx, write, current, "remove")
}

// AnnotationCount is how many annotations the cache holds.
func (c *Cache) AnnotationCount() int {
	c.annotations.mu.RLock()
	defer c.annotations.mu.RUnlock()

	return len(c.annotations.ordered)
}

// beginAnnotation takes the store before notifyMu, so waiting for it never
// holds up notify.
func (c *Cache) beginAnnotation(ctx context.Context) (AnnotationWrite, error) {
	c.annotations.mu.RLock()
	store := c.annotations.store
	c.annotations.mu.RUnlock()

	if store == nil {
		return nil, ErrAnnotationsOff
	}

	return store.BeginAnnotation(ctx)
}

// commitAnnotation commits a write, then indexes it and appends its item, all
// under notifyMu: every Append runs under it, so the ID taken from Count is
// the one the item gets. The index changes first, so a validator read from
// Count never names content the index does not hold yet.
func (c *Cache) commitAnnotation(
	ctx context.Context,
	write AnnotationWrite,
	a Annotation,
	action string,
) error {
	var err error
	if action == "remove" {
		err = write.Delete(ctx, a.ID)
	} else {
		err = write.Put(ctx, a)
	}

	if err != nil {
		return err
	}

	c.annotations.mu.Lock()
	i, found := c.annotations.find(a.ID)
	switch {
	case action == "remove":
		if found {
			c.annotations.ordered = slices.Delete(c.annotations.ordered, i, i+1)
		}
	case found:
		c.annotations.ordered[i] = a
	default:
		c.annotations.ordered = slices.Insert(c.annotations.ordered, i, a)
	}
	c.annotations.mu.Unlock()

	// The event shows the note as a read would, removal marks and all.
	marked := c.WithRemovals(a)
	id := strconv.FormatUint(a.ID, 10)
	historyID := c.history.Append(HistoryEntry{
		Kind:       KindAnnotation,
		Action:     action,
		ResourceID: id,
		Annotation: &marked,
	})

	c.dispatch(Event{
		Type:      EventAnnotation,
		Action:    action,
		ID:        id,
		Resource:  marked,
		HistoryID: historyID,
	})

	return nil
}

// AnnotationTag is a tag and how many annotations carry it.
type AnnotationTag struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// AnnotationTags counts the tags starting with prefix on the annotations
// readable admits, most used first, then by name. readable runs outside the
// lock, which is held only to copy the annotations carrying such a tag.
func (c *Cache) AnnotationTags(prefix string, readable func(Annotation) bool) []AnnotationTag {
	tagged := func(tag string) bool { return strings.HasPrefix(tag, prefix) }

	c.annotations.mu.RLock()
	var candidates []Annotation
	for _, a := range c.annotations.ordered {
		if slices.ContainsFunc(a.Tags, tagged) {
			candidates = append(candidates, a)
		}
	}
	c.annotations.mu.RUnlock()

	counts := map[string]int{}
	for _, a := range candidates {
		if !readable(a) {
			continue
		}

		for _, tag := range a.Tags {
			if tagged(tag) {
				counts[tag]++
			}
		}
	}

	tags := make([]AnnotationTag, 0, len(counts))
	for tag, count := range counts {
		tags = append(tags, AnnotationTag{Tag: tag, Count: count})
	}

	slices.SortFunc(tags, func(a, b AnnotationTag) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Tag, b.Tag))
	})

	return tags
}

// WithRemovals returns a marking each resource the cache no longer holds as
// removed; its resources are copied only when one is.
func (c *Cache) WithRemovals(a Annotation) Annotation {
	gone := slices.IndexFunc(a.Resources, func(r AnnotationResource) bool { return !c.holds(r) })
	if gone < 0 {
		return a
	}

	resources := slices.Clone(a.Resources)
	for i := range resources {
		resources[i].Removed = i == gone || i > gone && !c.holds(resources[i])
	}
	a.Resources = resources

	return a
}

func (c *Cache) holds(r AnnotationResource) bool {
	var ok bool
	switch r.Type {
	case EventService:
		_, ok = c.GetService(r.ID)
	case EventNode:
		_, ok = c.GetNode(r.ID)
	case EventStack:
		c.mu.RLock()
		_, ok = c.stacks[r.ID]
		c.mu.RUnlock()
	default:
		ok = true
	}

	return ok
}

// AnnotationsOn reports whether annotation writes have a store to go to.
func (c *Cache) AnnotationsOn() bool {
	c.annotations.mu.RLock()
	defer c.annotations.mu.RUnlock()

	return c.annotations.store != nil
}

// Timeline lists the history ring merged with the annotations, newest first by
// ID. An annotation sits at its creation ID with its current fields, and may be
// older than anything the ring still holds.
func (c *Cache) Timeline(q HistoryQuery) []HistoryEntry {
	if q.Limit <= 0 {
		q.Limit = 50
	}

	var ring []HistoryEntry
	if len(q.Kinds) != 1 || q.Kinds[0] != KindAnnotation {
		if q.Type == EventStack && q.ResourceID != "" {
			q.Members = c.stackMembers(q.ResourceID)
		}

		ring = c.history.List(q)
	}

	if !q.wantsKind(KindAnnotation) {
		return ring
	}

	notes := c.annotationEntries(q)
	merged := make([]HistoryEntry, 0, min(q.Limit, len(ring)+len(notes)))

	for len(merged) < q.Limit && (len(ring) > 0 || len(notes) > 0) {
		if len(notes) == 0 || len(ring) > 0 && ring[0].ID > notes[0].ID {
			merged, ring = append(merged, ring[0]), ring[1:]
		} else {
			merged, notes = append(merged, notes[0]), notes[1:]
		}
	}

	return merged
}

// stackMembers lists the services in stack and those the ring saw leave it.
func (c *Cache) stackMembers(stack string) []string {
	ids := c.history.removedFromStack(stack)

	c.mu.RLock()
	if s, ok := c.stacks[stack]; ok {
		ids = append(ids, s.Services...)
	}
	c.mu.RUnlock()

	return ids
}

// annotationEntries returns the newest q.Limit annotations q admits below
// q.BeforeID. ReadableAnnotation runs outside the lock, so the walk takes its
// matches in batches, each resuming below the last annotation it visited.
func (c *Cache) annotationEntries(q HistoryQuery) []HistoryEntry {
	q = q.prepared()

	var stack string
	if t := q.subjectType(); t == "" || t == EventService {
		stack = c.serviceStack(q.ResourceID)
	}

	entries := []HistoryEntry{}

	for cursor := q.BeforeID; len(entries) < q.Limit; {
		batch, next := c.annotationBatch(q, stack, cursor, q.Limit-len(entries))

		for _, a := range batch {
			if q.ReadableAnnotation == nil || q.ReadableAnnotation(a) {
				entries = append(entries, a.entry())
			}
		}

		if next == 0 {
			break
		}

		cursor = next
	}

	return entries
}

// annotationBatch collects up to n annotations q admits with an ID below
// cursor, or any ID for a zero cursor, newest first. next is the cursor to
// resume from, zero once nothing older is left.
func (c *Cache) annotationBatch(
	q HistoryQuery,
	stack string,
	cursor uint64,
	n int,
) (batch []Annotation, next uint64) {
	c.annotations.mu.RLock()
	defer c.annotations.mu.RUnlock()

	end := len(c.annotations.ordered)
	if cursor != 0 {
		end, _ = c.annotations.find(cursor)
	}

	for i := end - 1; i >= 0; i-- {
		a := c.annotations.ordered[i]
		if !q.matchesAnnotation(a, stack) {
			continue
		}

		if batch == nil {
			batch = make([]Annotation, 0, min(n, 64))
		}

		if batch = append(batch, a); len(batch) == n {
			return batch, a.ID
		}
	}

	return batch, 0
}

// matchesAnnotation applies q to a. A resource filter admits an annotation
// naming the resource, one on a service's stack or a stack's service, and a
// cluster-wide one; without a resource ID, the type is matched against the
// resources named.
func (q HistoryQuery) matchesAnnotation(a Annotation, stack string) bool {
	end := a.Timestamp
	if !a.Until.IsZero() {
		end = a.Until
	}

	if !q.After.IsZero() && !end.After(q.After) ||
		!q.Before.IsZero() && a.Timestamp.After(q.Before) {
		return false
	}

	if !q.matchesTags(a.Tags) || !q.matchesAuthor(a.Actor) {
		return false
	}

	if q.ResourceID != "" {
		if !a.About(q.subjectType(), q.ResourceID, stack) {
			return false
		}
	} else if len(a.Resources) > 0 &&
		!slices.ContainsFunc(a.Resources, func(r AnnotationResource) bool {
			return (q.Type == "" || r.Type == q.Type) &&
				(len(q.Types) == 0 || slices.Contains(q.Types, r.Type))
		}) {
		return false
	}

	return q.annotationMatchesText(a)
}

// annotationMatchesText applies TitleContains to a's text, and Search to
// anything a reader sees of it, each word anywhere.
func (q HistoryQuery) annotationMatchesText(a Annotation) bool {
	if q.TitleContains == "" && q.Search == "" {
		return true
	}

	if !q.ready {
		q = q.prepared()
	}

	return ContainsLower(a.Text, q.TitleContains) &&
		!slices.ContainsFunc(q.words, func(word string) bool {
			has := func(field string) bool { return ContainsLower(field, word) }

			return !has(a.Text) && !slices.ContainsFunc(a.Tags, has) && !actorHas(a.Actor, word) &&
				!slices.ContainsFunc(
					a.Resources,
					func(r AnnotationResource) bool { return nameHas(r.Name, word) },
				)
		})
}

// subjectType is the type of the resource q's ResourceID names, when q says
// which; only a stack's is told apart, and a stack's timeline asks for one.
func (q HistoryQuery) subjectType() EventType {
	if q.Type != "" {
		return q.Type
	}

	if len(q.Types) == 1 {
		return q.Types[0]
	}

	return ""
}

// AnnotationOn reports whether a belongs on the timeline of the resource of
// typ with id, as a list filtered by that resource finds it. A service's stack
// is looked up only for an annotation naming a stack.
func (c *Cache) AnnotationOn(a Annotation, typ EventType, id string) bool {
	var stack string
	if typ == EventService && slices.ContainsFunc(a.Resources, func(r AnnotationResource) bool {
		return r.Type == EventStack
	}) {
		stack = c.serviceStack(id)
	}

	return a.About(typ, id, stack)
}

// serviceStack is the stack of the service with id, or empty. A removed
// service's comes from its removal, while the ring holds it.
func (c *Cache) serviceStack(id string) string {
	if id == "" {
		return ""
	}

	if svc, ok := c.GetService(id); ok {
		return svc.Spec.Labels[StackLabel]
	}

	removal := c.history.List(HistoryQuery{
		ResourceID: id, Type: EventService, Kinds: []string{KindChange}, Limit: 1,
	})
	if len(removal) == 0 {
		return ""
	}

	return removal[0].Stack
}

// About reports whether a belongs on the timeline of the resource of typ with
// id: it is cluster-wide, it names the resource, it names stack (a service's
// stack), or, on a stack's timeline, it names one of the stack's services.
func (a Annotation) About(typ EventType, id, stack string) bool {
	return len(a.Resources) == 0 ||
		slices.ContainsFunc(a.Resources, func(r AnnotationResource) bool {
			return r.ID == id ||
				stack != "" && r.Type == EventStack && r.ID == stack ||
				typ == EventStack && r.Type == EventService && r.Stack == id
		})
}

func (a Annotation) entry() HistoryEntry {
	return HistoryEntry{
		ID:        a.ID,
		Kind:      KindAnnotation,
		Timestamp: a.Timestamp,
		Until:     a.Until,
		Actor:     a.Actor,
		Text:      a.Text,
		Tags:      a.Tags,
		Resources: a.Resources,
		Updated:   a.Updated,
		Editor:    a.Editor,

		Annotation: &a,
	}
}
