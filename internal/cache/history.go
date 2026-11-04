package cache

import (
	"math"
	"slices"
	"strings"
	"sync"
	"time"
)

// Kinds lists every kind of timeline item, in the order the docs name them.
var Kinds = []string{KindChange, KindGap, KindAnnotation}

// The kinds of timeline item. A reader skips a kind it does not know.
const (
	KindChange = "change"
	KindGap    = "gap"

	// KindAnnotation is a write to an annotation, for SSE and replay alone:
	// lists read the annotation index, and the writer skips these.
	KindAnnotation = "annotation"
)

type HistoryEntry struct {
	ID        uint64    `json:"id"`
	Kind      string    `json:"kind"`
	Timestamp time.Time `json:"timestamp"`

	// Until ends a gap, which runs from Timestamp; zero for a change.
	Until time.Time `json:"until,omitzero"`

	// RecoveredFrom is when a gap's recovered changes become complete for every
	// type but tasks, which Docker records no events for; zero when none were.
	RecoveredFrom time.Time `json:"recoveredFrom,omitzero"`

	// Recovered marks a change read back from Docker's event buffer after a
	// gap rather than watched: it has no actor and no summary.
	Recovered bool `json:"recovered,omitempty"`

	Type       EventType `json:"type"`
	Action     string    `json:"action"`
	ResourceID string    `json:"resourceId"`
	Name       string    `json:"name"`
	Summary    string    `json:"summary,omitempty"`

	// Actor is who made the change through Cetacean; nil for one made elsewhere.
	Actor *Actor `json:"actor,omitempty"`

	// Service and Stack are the remove Event's, for authorizing its replay.
	Service string `json:"-"`
	Stack   string `json:"-"`

	// ServiceID is the owning service of a task entry. A query for the service
	// returns it too, since a crash loop changes nothing on the service itself.
	ServiceID string `json:"-"`

	// Slot, NodeID and ServiceName are a task entry's replica and its service's
	// name, which name it once Swarm has pruned the task or its service; all are
	// zero on an entry recorded before they were kept.
	Slot        int    `json:"-"`
	NodeID      string `json:"-"`
	ServiceName string `json:"-"`

	// The annotation's own fields, set on the entry a list makes of one.
	Text      string               `json:"text,omitempty"`
	Tags      []string             `json:"tags,omitempty"`
	Resources []AnnotationResource `json:"resources,omitempty"`
	Updated   time.Time            `json:"updated,omitzero"`
	Editor    *Actor               `json:"editor,omitempty"`

	// Annotation is the one an annotation entry stands for: on a ring item,
	// as its write left it or, on a remove, as it was.
	Annotation *Annotation `json:"-"`

	// Previous is, on an annotation update's ring item, what the update replaced.
	Previous *Annotation `json:"-"`
}

type HistoryQuery struct {
	Type       EventType
	ResourceID string
	BeforeID   uint64 // keeps the entries below it; zero means unbounded

	// TitleContains narrows, ignoring case, to a change whose name holds it or
	// an annotation whose text does: what heads the row.
	TitleContains string

	// Search narrows to the entries that mention each of its words, ignoring
	// case, anywhere a row shows: a change's name, ID prefix, type, action and
	// actor, or an annotation's text, tags, resources and author. A gap passes.
	Search string
	Limit  int

	// TaskName, when set, names a task entry as its row does, for the text
	// filters above: a task records its ID as its name. It may take the cache's lock.
	TaskName func(e HistoryEntry) string

	// words is Search folded and split, and ready says the text filters are
	// folded; a walk prepares them once, and a matcher called alone does.
	words []string
	ready bool

	// Types narrows to entries of any of these types, where Type narrows to
	// exactly one; empty means every type, and both set means the intersection.
	// Pushed down rather than applied by the caller so the walk copies only
	// what is returned, under the read lock every Append contends with.
	Types []EventType

	// Kinds narrows to entries of any of these kinds; empty means every kind.
	// It is the only filter besides the window and Visible that a gap does not
	// pass: a gap names no resource, and nothing was recorded while it lasted.
	Kinds []string

	// Before bounds the result at the newer end: only entries starting at or
	// before it. Unlike After it cannot end the walk — entries are visited
	// newest-first, so the ones this excludes come first — so it skips.
	// Zero means unbounded.
	Before time.Time

	// After ends the walk at the first entry ending at or before it, instead
	// of copying the whole ring to discard it: ends never decrease from one ID
	// to the next, though a gap's start can. Exclusive; zero means unbounded.
	// Named for the comparison, since "since" here means History.Since.
	After time.Time

	// Visible, when set, keeps only the entries it admits, so Limit and
	// BeforeID page over what the caller may read. It runs under the history
	// lock and must not call back into History.
	Visible func(HistoryEntry) bool

	// Tags narrows to annotations carrying every one of them, or any with
	// MatchAny. A gap passes, as it does search; a change carries no tags.
	Tags     []string
	MatchAny bool

	// Authors narrows to what an actor with one of these subjects did: the
	// changes made through Cetacean and the annotations written. A gap passes.
	Authors []string

	// ReadableAnnotation, when set, keeps only the annotations it admits. It
	// runs outside every cache lock.
	ReadableAnnotation func(Annotation) bool

	// Members, on a stack's query, are its services' IDs, whose changes its
	// timeline holds as a service's holds its tasks', whatever Types says.
	// Cache.Timeline sets it for a Type or one of Types that is a stack.
	Members []string
}

type History struct {
	mu      sync.RWMutex
	entries []HistoryEntry
	size    int
	cursor  int
	count   uint64
	full    bool

	// byResource maps resource IDs to a ring of entry indices, enabling fast
	// filtered lookups without a scan. Stale indices are skipped; an ID leaves
	// the map once the ring overwrites its newest entry. Gaps name no resource
	// and sit under the empty ID.
	byResource map[string]*indexRing
}

// indexRing is a small ring buffer of int indices into History.entries.
type indexRing struct {
	indices []int
	cursor  int
	full    bool
}

const indexRingSize = 64

// maxListPrealloc bounds what List reserves up front. A caller filtering after
// the read passes the ring's whole size as the limit, but is usually bounded by
// `After` and stops within a few entries. A read that genuinely walks the ring
// end to end still grows to fit; it pays for the growth, not the reservation.
const maxListPrealloc = 512

func (r *indexRing) push(idx int) {
	r.indices[r.cursor] = idx
	r.cursor++
	if r.cursor >= len(r.indices) {
		r.cursor = 0
		r.full = true
	}
}

func (r *indexRing) newest() int {
	if r.cursor == 0 {
		return r.indices[len(r.indices)-1]
	}

	return r.indices[r.cursor-1]
}

func NewHistory(size int) *History {
	return &History{
		entries:    make([]HistoryEntry, size),
		size:       size,
		byResource: make(map[string]*indexRing),
	}
}

// Size is the ring's capacity — the most entries List can ever return. A
// caller applying its own filters after the read needs it, or it has to guess
// a window and silently under-report whatever falls outside it.
func (h *History) Size() int {
	return h.size
}

// Oldest is the timestamp of the oldest entry the ring still holds, restored
// ones included, and so the start of the only window a query over it can
// honestly answer for. Nothing in a result otherwise shows where the record
// begins. ok=false when nothing is recorded.
func (h *History) Oldest() (time.Time, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.cursor == 0 && !h.full {
		return time.Time{}, false
	}

	// Before the ring wraps the oldest entry is at index 0; after it wraps the
	// cursor points at the slot about to be overwritten, which is the oldest.
	oldest := 0
	if h.full {
		oldest = h.cursor
	}

	return h.entries[oldest].Timestamp, true
}

func (h *History) Count() uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.count
}

// Since returns all entries with ID > afterID in chronological order.
// Returns ok=false if afterID is a future ID, or if entries after it have been
// overwritten, meaning the caller cannot trust the result is complete; in the
// latter case the result is what the ring still holds.
func (h *History) Since(afterID uint64) ([]HistoryEntry, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Future ID or empty history
	if afterID > h.count {
		return nil, false
	}

	// Caught up — no new entries
	if afterID == h.count {
		return nil, true
	}

	// Read, not derived from count and size: restored IDs may jump.
	oldest := 0
	if h.full {
		oldest = h.cursor
	}

	// The entry right after afterID has been overwritten — zero included.
	complete := afterID+1 >= h.entries[oldest].ID

	slots := h.size
	if !h.full {
		slots = h.cursor
	}

	var result []HistoryEntry

	// Newest first, so a caller close behind visits only what it lacks.
	for i := range slots {
		index := h.cursor - 1 - i
		if index < 0 {
			index += h.size
		}

		e := h.entries[index]
		if e.ID <= afterID {
			break
		}

		result = append(result, e)
	}

	slices.Reverse(result)

	return result, complete
}

// Restore fills an empty ring, oldest first, with the newest entries that fit;
// it must precede any Append. Count skips a ring's worth past last, the largest
// ID stored of any kind, since a crashed process handed out IDs it never wrote
// and a reader has seen them.
func (h *History) Restore(entries []HistoryEntry, last uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(entries) > h.size {
		entries = entries[len(entries)-h.size:]
	}

	for _, e := range entries {
		h.entries[h.cursor] = e
		h.index(e.ResourceID)
		if e.ServiceID != "" {
			h.index(e.ServiceID)
		}

		h.cursor++
		if h.cursor >= h.size {
			h.cursor = 0
			h.full = true
		}

		h.count = e.ID
	}

	h.count = max(h.count, last)
	if h.count > 0 {
		h.count += uint64(h.size)
	}
}

func (h *History) Append(e HistoryEntry) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count++
	e.ID = h.count
	if e.Kind == "" {
		e.Kind = KindChange
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}

	// The ring overwrites oldest-first, so a resource whose newest entry sits
	// in the slot being reused has no entry left.
	if h.full {
		evicted := h.entries[h.cursor]
		for _, id := range [...]string{evicted.ResourceID, evicted.ServiceID} {
			if ring := h.byResource[id]; ring != nil && ring.newest() == h.cursor {
				delete(h.byResource, id)
			}
		}
	}

	h.entries[h.cursor] = e

	// Update the per-resource index, under the owning service as well. An
	// annotation item's ResourceID is the annotation's, which names no resource.
	if e.Kind != KindAnnotation {
		h.index(e.ResourceID)
		if e.ServiceID != "" {
			h.index(e.ServiceID)
		}
	}

	h.cursor++
	if h.cursor >= h.size {
		h.cursor = 0
		h.full = true
	}

	return h.count
}

func (h *History) index(id string) {
	ring := h.byResource[id]
	if ring == nil {
		ring = &indexRing{indices: make([]int, indexRingSize)}
		h.byResource[id] = ring
	}
	ring.push(h.cursor)
}

// about reports whether an entry belongs on id's timeline.
func (e HistoryEntry) about(id string) bool {
	return e.ResourceID == id || e.ServiceID == id
}

// removedFromStack lists the services whose removal the ring recorded in stack.
func (h *History) removedFromStack(stack string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var ids []string
	for _, e := range h.entries {
		if e.Type == EventService && e.Action == "remove" && e.Stack == stack {
			ids = append(ids, e.ResourceID)
		}
	}

	return ids
}

// end is when an entry stops: a gap's Until, otherwise its Timestamp.
func (e HistoryEntry) end() time.Time {
	if e.Until.IsZero() {
		return e.Timestamp
	}

	return e.Until
}

func (h *History) List(q HistoryQuery) []HistoryEntry {
	q = q.prepared()

	h.mu.RLock()
	defer h.mu.RUnlock()

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}

	// Fast path: a resource-ID filter reads the per-resource index, but only
	// when it can prove it answered the whole question — it holds the newest
	// indexRingSize entries of one resource, so a stack's members are not in it,
	// and runs out on a larger limit and on a cursor paging off its end.
	if q.ResourceID != "" && len(q.Members) == 0 {
		if found, complete := h.listByResource(q, limit); complete {
			return found
		}
	}

	result := make([]HistoryEntry, 0, min(limit, maxListPrealloc))

	// Iterate newest-first from cursor-1 backwards
	slots := h.size
	if !h.full {
		slots = h.cursor
	}

	for i := range slots {
		idx := h.cursor - 1 - i
		if idx < 0 {
			idx += h.size
		}

		e := h.entries[idx]
		if q.BeforeID != 0 && e.ID >= q.BeforeID {
			continue
		}

		// Newest-first, so everything past this point ends earlier still.
		if !q.After.IsZero() && !e.end().After(q.After) {
			break
		}

		if !q.matches(e) {
			continue
		}

		result = append(result, e)

		if len(result) == limit {
			break
		}
	}

	return result
}

// matches applies the filters that skip an entry rather than end the walk. The
// two list paths share it so a filter added here is honoured on both, which is
// the same reason listByResource takes the whole query.
func (q HistoryQuery) matches(e HistoryEntry) bool {
	if e.Kind == KindAnnotation || !q.wantsKind(e.Kind) {
		return false
	}

	if !q.Before.IsZero() && e.Timestamp.After(q.Before) {
		return false
	}

	if e.Kind == KindChange && (len(q.Tags) > 0 || !q.matchesAuthor(e.Actor)) {
		return false
	}

	if e.Kind != KindGap && !q.matchesResource(e) {
		return false
	}

	// Visible goes first: the text filters may resolve a task's name.
	return (q.Visible == nil || q.Visible(e)) && (e.Kind == KindGap || q.matchesText(e))
}

// wantsKind reports whether q admits entries of kind.
func (q HistoryQuery) wantsKind(kind string) bool {
	return len(q.Kinds) == 0 || slices.Contains(q.Kinds, kind)
}

// matchesTags reports whether tags satisfy q's tag filter.
func (q HistoryQuery) matchesTags(tags []string) bool {
	if len(q.Tags) == 0 {
		return true
	}

	carried := func(tag string) bool { return slices.Contains(tags, tag) }
	if q.MatchAny {
		return slices.ContainsFunc(q.Tags, carried)
	}

	return !slices.ContainsFunc(q.Tags, func(tag string) bool { return !carried(tag) })
}

// matchesAuthor reports whether actor satisfies q's author filter.
func (q HistoryQuery) matchesAuthor(actor *Actor) bool {
	return len(q.Authors) == 0 || actor != nil && slices.Contains(q.Authors, actor.Subject)
}

// matchesResource applies the filters that select by resource.
func (q HistoryQuery) matchesResource(e HistoryEntry) bool {
	member := e.Type == EventService && slices.Contains(q.Members, e.ResourceID)

	if q.ResourceID != "" && !e.about(q.ResourceID) && !member {
		return false
	}

	if q.Type != "" && e.Type != q.Type && !member {
		return false
	}

	return len(q.Types) == 0 || slices.Contains(q.Types, e.Type) || member
}

// matchesText applies TitleContains and Search to a change.
func (q HistoryQuery) matchesText(e HistoryEntry) bool {
	if q.TitleContains == "" && q.Search == "" {
		return true
	}

	if !q.ready {
		q = q.prepared()
	}

	name := e.Name
	if e.Type == EventTask && q.TaskName != nil {
		name = q.TaskName(e)
	}

	return ContainsLower(name, q.TitleContains) &&
		!slices.ContainsFunc(q.words, func(word string) bool {
			return !nameHas(name, word) && !hasPrefixLower(e.ResourceID, word) &&
				!ContainsLower(string(e.Type), word) && !ContainsLower(e.Action, word) &&
				!actorHas(e.Actor, word)
		})
}

// prepared folds q's text filters and splits Search into its words.
func (q HistoryQuery) prepared() HistoryQuery {
	q.TitleContains = strings.ToLower(q.TitleContains)
	q.words = strings.Fields(strings.ToLower(q.Search))
	q.ready = true

	return q
}

// nameHas reports whether word is in name as stored or as the timeline shows it.
func nameHas(name, word string) bool {
	return ContainsLower(name, word) ||
		strings.Contains(word, "/") && ContainsLower(stackDisplay(name), word)
}

// actorHas reports whether word is in actor's name or subject.
func actorHas(actor *Actor, word string) bool {
	return actor != nil && (ContainsLower(actor.Name, word) || ContainsLower(actor.Subject, word))
}

// hasPrefixLower reports whether s starts with prefixLower, ignoring ASCII case.
func hasPrefixLower(s, prefixLower string) bool {
	return len(prefixLower) <= len(s) && ContainsLower(s[:len(prefixLower)], prefixLower)
}

// ContainsLower reports whether s contains substrLower (already lowercased),
// ignoring ASCII case and without allocating.
func ContainsLower(s, substrLower string) bool {
	for i := 0; i <= len(s)-len(substrLower); i++ {
		j := 0

		for j < len(substrLower) {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}

			if c != substrLower[j] {
				break
			}

			j++
		}

		if j == len(substrLower) {
			return true
		}
	}

	return false
}

// stackDisplay is a name as the dashboard's timeline shows it: split at its
// first underscore, past the first character, into a stack and the rest.
func stackDisplay(name string) string {
	if i := strings.IndexByte(name, '_'); i > 0 {
		return name[:i] + "/" + name[i+1:]
	}

	return name
}

// listByResource answers a query already known to name a resource, taking the
// whole query rather than five fields so a new one is honoured on both paths or
// neither. complete says whether the answer is the whole one: the index is a
// fixed window, so only the caller's fallback to a scan separates the cases.
func (h *History) listByResource(
	q HistoryQuery,
	limit int,
) (entries []HistoryEntry, complete bool) {
	own := h.indexCursor(q.ResourceID, func(e HistoryEntry) bool { return e.about(q.ResourceID) })

	// Every gap belongs on every resource's timeline, so its index is merged in.
	gaps := &indexCursor{whole: true}
	if q.wantsKind(KindGap) {
		gaps = h.indexCursor("", func(e HistoryEntry) bool { return e.Kind == KindGap })
	}

	result := make([]HistoryEntry, 0, min(limit, indexRingSize))

	for len(result) < limit {
		// An index that has wrapped may have lost older entries that would
		// sort between the other's, so running out of one ends the answer.
		if !own.ok && !own.whole || !gaps.ok && !gaps.whole {
			return result, false
		}

		var e HistoryEntry

		switch {
		case own.ok && (!gaps.ok || own.head.ID > gaps.head.ID):
			e = own.next()
		case gaps.ok:
			e = gaps.next()
		default:
			return result, true
		}

		if q.BeforeID != 0 && e.ID >= q.BeforeID {
			continue
		}

		// Newest-first here too, so this ends the walk rather than skipping.
		if !q.After.IsZero() && !e.end().After(q.After) {
			return result, true
		}

		if q.matches(e) {
			result = append(result, e)
		}
	}

	return result, true
}

// indexCursor walks one index newest first, one entry ahead: head is the next
// entry when ok. whole says the index never wrapped, so it names everything
// the ring holds under its ID.
type indexCursor struct {
	history *History
	ring    *indexRing
	belongs func(HistoryEntry) bool
	visited int
	last    uint64

	head      HistoryEntry
	ok, whole bool
}

// indexCursor starts on id's index. A missing index is proof nothing under
// the ID survives: an index is pruned only once its newest entry is overwritten.
func (h *History) indexCursor(id string, belongs func(HistoryEntry) bool) *indexCursor {
	ring := h.byResource[id]
	cursor := &indexCursor{
		history: h,
		ring:    ring,
		belongs: belongs,
		last:    math.MaxUint64,
		whole:   ring == nil || !ring.full,
	}
	cursor.advance()

	return cursor
}

// next returns head and moves past it.
func (c *indexCursor) next() HistoryEntry {
	e := c.head
	c.advance()

	return e
}

// advance finds the next entry. Newest first, the entries an index still names
// have falling IDs; a slot overwritten since it was indexed holds another
// entry, or a newer one under this ID that an earlier reference yielded.
func (c *indexCursor) advance() {
	c.ok = false

	if c.ring == nil {
		return
	}

	total := len(c.ring.indices)
	if !c.ring.full {
		total = c.ring.cursor
	}

	for c.visited < total {
		index := c.ring.cursor - 1 - c.visited
		if index < 0 {
			index += len(c.ring.indices)
		}

		c.visited++

		if e := c.history.entries[c.ring.indices[index]]; c.belongs(e) && e.ID < c.last {
			c.last, c.head, c.ok = e.ID, e, true
			return
		}
	}
}
