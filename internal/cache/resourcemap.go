package cache

import (
	"cmp"
	"maps"
	"slices"
	"sync"
)

// ResourceMap is a typed map for a single resource type that shares the
// parent Cache's RWMutex. The optional onSet and onDelete hooks run inside the
// write lock to maintain derived state; onSet gets a pointer so it can mutate
// the value before storage (e.g., clearing secret data).
type ResourceMap[T any] struct {
	mu       *sync.RWMutex
	items    map[string]T
	nameFunc func(T) string                    // extracts display name for events
	onSet    func(key string, old *T, new_ *T) // called under write lock; old is nil on first set
	onDelete func(key string, old T)           // called under write lock

	// version, when set, orders writes: Set ignores a value older than the
	// one it would replace, which an inspect overtaken by a later one carries.
	version func(T) uint64
}

func (r *ResourceMap[T]) set(key string, v *T) {
	if r.onSet != nil {
		if old, ok := r.items[key]; ok {
			r.onSet(key, &old, v)
		} else {
			r.onSet(key, nil, v)
		}
	}
	r.items[key] = *v
}

func (r *ResourceMap[T]) get(key string) (T, bool) {
	v, ok := r.items[key]
	return v, ok
}

func (r *ResourceMap[T]) del(key string) (name string) {
	if old, ok := r.items[key]; ok {
		if r.nameFunc != nil {
			name = r.nameFunc(old)
		}
		if r.onDelete != nil {
			r.onDelete(key, old)
		}
	}
	delete(r.items, key)
	return name
}

// listSorted returns every value ordered by map key (the resource ID, or the
// name for volumes). Go randomizes map iteration, so without this the API's
// stable sort would order equal sort keys differently on every request — and a
// client paging through a list would see items shift between pages.
func listSorted[T any](items map[string]T) []T {
	out := make([]T, 0, len(items))
	for _, k := range slices.Sorted(maps.Keys(items)) {
		out = append(out, items[k])
	}
	return out
}

// list is listSorted over the map itself. Caller must hold the lock; List
// takes the same order without holding it across the sort.
func (r *ResourceMap[T]) list() []T {
	return listSorted(r.items)
}

// Add stores v only while key is absent, so a record already held is never
// replaced; ok is false when it was.
func (r *ResourceMap[T]) Add(key string, v T, eventType EventType) (event Event, ok bool) {
	r.mu.Lock()
	if _, existed := r.items[key]; existed {
		r.mu.Unlock()
		return Event{}, false
	}
	r.set(key, &v)
	r.mu.Unlock()

	var name string
	if r.nameFunc != nil {
		name = r.nameFunc(v)
	}

	return Event{Type: eventType, Action: setAction(false), ID: key, Name: name, Resource: v}, true
}

// Set stores a value, calling the onSet hook under the write lock. The event
// reports whether the key was new, which is what lets a client distinguish a
// resource appearing from one it already counts changing. ok is false when the
// value was older than the stored one and nothing changed.
func (r *ResourceMap[T]) Set(key string, v T, eventType EventType) (event Event, ok bool) {
	r.mu.Lock()
	old, existed := r.items[key]
	if existed && r.version != nil && r.version(v) < r.version(old) {
		r.mu.Unlock()
		return Event{}, false
	}
	r.set(key, &v)
	r.mu.Unlock()
	var name string
	if r.nameFunc != nil {
		name = r.nameFunc(v)
	}
	event = Event{
		Type:     eventType,
		Action:   setAction(existed),
		ID:       key,
		Name:     name,
		Resource: v,
	}
	if existed && (eventType == EventConfig || eventType == EventSecret) {
		event.Previous = old
	}

	return event, true
}

// Get retrieves a value by key.
func (r *ResourceMap[T]) Get(key string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.get(key)
}

// Delete removes a value by key, calling the onDelete hook.
func (r *ResourceMap[T]) Delete(key string, eventType EventType) Event {
	r.mu.Lock()
	old, existed := r.items[key]
	name := r.del(key)
	r.mu.Unlock()
	event := Event{Type: eventType, Action: "remove", ID: key, Name: name}
	if existed {
		event.Resource = old
	}
	return event
}

// List returns all values as a slice, ordered by map key like list. The copy
// is taken under the read lock and sorted after releasing it, keeping the sort
// off the writers' lock. Keys and values go into parallel slices: cloning the
// map would allocate each large value separately.
func (r *ResourceMap[T]) List() []T {
	r.mu.RLock()
	keys := make([]string, 0, len(r.items))
	vals := make([]T, 0, len(r.items))
	for k, v := range r.items {
		keys = append(keys, k)
		vals = append(vals, v)
	}
	r.mu.RUnlock()

	sortByIndex(vals, func(a, b int) int { return cmp.Compare(keys[a], keys[b]) })

	return vals
}

// sortByIndex reorders vals in place by sorting their positions with compare,
// so the sort swaps ints rather than the values, and then moves each value at
// most once by following the permutation's cycles — no second copy of vals.
func sortByIndex[T any](vals []T, compare func(a, b int) int) {
	order := make([]int, len(vals))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, compare)

	// Position i takes vals[order[i]]; a finished position is marked order[i] = i.
	for start := range order {
		if order[start] == start {
			continue
		}

		held := vals[start]
		for at := start; ; {
			from := order[at]
			order[at] = at
			if from == start {
				vals[at] = held
				break
			}
			vals[at] = vals[from]
			at = from
		}
	}
}

// Each calls yield for every value in List's order, stopping early if yield
// returns false. yield runs under the read lock and must not call back into the
// cache: RWMutex readers are not re-entrant, so a waiting writer deadlocks it.
// The sort stays under the lock so the scan reports one snapshot.
func (r *ResourceMap[T]) Each(yield func(T) bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	keys := make([]string, 0, len(r.items))
	for k := range r.items {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, k := range keys {
		if !yield(r.items[k]) {
			return
		}
	}
}

// Len returns the number of items.
func (r *ResourceMap[T]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.items)
}

// Replace atomically swaps the entire map contents, keeping the stored value
// wherever m holds an older version of it. Hooks are not called. Caller must
// hold the write lock.
func (r *ResourceMap[T]) Replace(m map[string]T) {
	keepNewer(m, r.items, r.version)
	r.items = m
}

// keepNewer overwrites each entry of listed with cached's where cached holds a
// newer version: a list can be read before an inspect that already landed.
// Keys only cached holds stay out, as the list decides what exists. A nil
// version keeps nothing.
func keepNewer[T any](listed, cached map[string]T, version func(T) uint64) {
	if version == nil {
		return
	}

	for key, fresh := range listed {
		if held, ok := cached[key]; ok && version(held) > version(fresh) {
			listed[key] = held
		}
	}
}
