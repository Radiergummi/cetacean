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

// list returns every value ordered by map key (the resource ID, or the name
// for volumes). Go randomizes map iteration, so without this the API's stable
// sort would order equal sort keys differently on every request — and a client
// paging through a list would see items shift between pages. Caller must hold
// the lock; List takes the same order without holding it across the sort.
func (r *ResourceMap[T]) list() []T {
	out := make([]T, 0, len(r.items))
	for _, k := range slices.Sorted(maps.Keys(r.items)) {
		out = append(out, r.items[k])
	}
	return out
}

// Set stores a value, calling the onSet hook under the write lock. The event
// reports whether the key was new, which is what lets a client distinguish a
// resource appearing from one it already counts changing.
func (r *ResourceMap[T]) Set(key string, v T, eventType EventType) Event {
	r.mu.Lock()
	_, existed := r.items[key]
	r.set(key, &v)
	r.mu.Unlock()
	var name string
	if r.nameFunc != nil {
		name = r.nameFunc(v)
	}
	return Event{Type: eventType, Action: setAction(existed), ID: key, Name: name, Resource: v}
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
	name := r.del(key)
	r.mu.Unlock()
	return Event{Type: eventType, Action: "remove", ID: key, Name: name}
}

// List returns all values as a slice, ordered by map key like list.
//
// The copy comes out under the read lock and is sorted after releasing it: the
// sort is the expensive half of the call on a large cluster, and it does not
// belong under the lock the watcher's writers are contending for. ListServices
// and ListTasks split the work the same way.
func (r *ResourceMap[T]) List() []T {
	type entry struct {
		key   string
		value T
	}

	r.mu.RLock()
	entries := make([]entry, 0, len(r.items))
	for key, value := range r.items {
		entries = append(entries, entry{key: key, value: value})
	}
	r.mu.RUnlock()

	slices.SortFunc(entries, func(a, b entry) int { return cmp.Compare(a.key, b.key) })

	out := make([]T, len(entries))
	for i, e := range entries {
		out[i] = e.value
	}
	return out
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
