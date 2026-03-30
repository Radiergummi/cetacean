package cache

import "sync"

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
	onSet    func(key string, old *T, new_ *T) // called under write lock; old is nil on first set
	onDelete func(key string, old T)           // called under write lock
}

func newResourceMap[T any](
	mu *sync.RWMutex,
	onSet func(key string, old *T, new_ *T),
	onDelete func(key string, old T),
) ResourceMap[T] {
	return ResourceMap[T]{
		mu:       mu,
		items:    make(map[string]T),
		onSet:    onSet,
		onDelete: onDelete,
	}
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

func (r *ResourceMap[T]) del(key string) {
	if r.onDelete != nil {
		if old, ok := r.items[key]; ok {
			r.onDelete(key, old)
		}
	}
	delete(r.items, key)
}

func (r *ResourceMap[T]) list() []T {
	out := make([]T, 0, len(r.items))
	for _, v := range r.items {
		out = append(out, v)
	}
	return out
}

// Set stores a value, calling the onSet hook and notifying via the returned Event.
func (r *ResourceMap[T]) Set(key string, v T, eventType string) Event {
	r.mu.Lock()
	r.set(key, &v)
	r.mu.Unlock()
	return Event{Type: eventType, Action: "update", ID: key, Resource: v}
}

// Get retrieves a value by key.
func (r *ResourceMap[T]) Get(key string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.get(key)
}

// Delete removes a value by key, calling the onDelete hook.
func (r *ResourceMap[T]) Delete(key string, eventType string) Event {
	r.mu.Lock()
	r.del(key)
	r.mu.Unlock()
	return Event{Type: eventType, Action: "remove", ID: key}
}

// List returns all values as a slice.
func (r *ResourceMap[T]) List() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.list()
}

// Len returns the number of items.
func (r *ResourceMap[T]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.items)
}

// Replace atomically swaps the entire map contents. Hooks are not called.
// Caller must hold the write lock.
func (r *ResourceMap[T]) replace(items map[string]T) {
	r.items = items
}
