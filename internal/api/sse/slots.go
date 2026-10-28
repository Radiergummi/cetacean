package sse

import (
	"net/http"
	"sync"
)

// Slots caps concurrent work as the connection cap does: max in all, and a
// quarter of that per caller, so no one identity, or address under auth none,
// can hold every slot. The zero value is ready to use.
type Slots struct {
	mu     sync.Mutex
	total  int
	owners map[string]int
}

// Acquire takes a slot for r's caller, or reports whether the whole cap or the
// caller's share is what is full. release gives the slot back.
func (s *Slots) Acquire(r *http.Request, max int) (release func(), callerFull, ok bool) {
	owner := ownerOf(r)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.total >= max {
		return nil, false, false
	}
	if s.owners[owner] >= max/4 {
		return nil, true, false
	}

	if s.owners == nil {
		s.owners = make(map[string]int)
	}
	s.total++
	s.owners[owner]++

	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.total--
		if s.owners[owner]--; s.owners[owner] <= 0 {
			delete(s.owners, owner)
		}
	}, false, true
}

// Full reports what Acquire would refuse for r's caller, without taking a slot.
func (s *Slots) Full(r *http.Request, max int) (callerFull, full bool) {
	owner := ownerOf(r)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.total >= max {
		return false, true
	}

	return s.owners[owner] >= max/4, s.owners[owner] >= max/4
}

// InUse is how many slots are taken.
func (s *Slots) InUse() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.total
}
