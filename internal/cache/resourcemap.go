package cache

import (
	"cmp"
	"maps"
	"slices"
	"sync"
)

// ResourceMap is a typed map for a single resource type that shares the
// parent Cache's RWMutex. It provides the common Set/Get/Delete/List/Replace
// operations that every cached Docker resource needs.
//
// The optional onSet and onDelete hooks run inside the write lock and are used
// by the Cache to maintain derived state (stack membership, secret scrubbing).
// The onSet hook receives a pointer to the new value so it can mutate it
// before storage (e.g., clearing secret data).
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
	return Event{Type: eventType, Action: setAction(existed), ID: key, Name: name, Resource: v}, true
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

// List returns all values as a slice, ordered by map key like list.
//
// The copy comes out under the read lock and is sorted after releasing it: the
// sort is the expensive half of the call on a large cluster, and it does not
// belong under the lock the watcher's writers are contending for. ListServices
// and ListTasks split the work the same way.
//
// Keys and values are gathered into parallel slices rather than cloning the
// map: Go stores map values larger than 128 bytes indirectly, so cloning one
// allocates every value separately. Sorting an index permutation then keeps
// the sort itself off the values, which are wide enough that swapping them
// costs more than the index.
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

// Each calls yield for every value in the same order List returns them,
// stopping early if yield returns false. Only the keys are copied, so a scan
// that keeps few of the values — search — allocates a fraction of what listing
// them costs.
//
// yield runs under the read lock and must not call back into the cache: Go's
// RWMutex is not re-entrant for readers, so a writer arriving between the two
// acquisitions deadlocks the second. Collect what matches and enrich after.
//
// The sort stays under the lock, where List's does not: the yield holds it
// either way, and a scan that released it would no longer report one snapshot.
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

// Replace atomically swaps the entire map contents. Hooks are not called.
// Caller must hold the write lock.
func (r *ResourceMap[T]) Replace(m map[string]T) {
	r.items = m
}
