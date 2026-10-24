package cache

import (
	"strconv"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// A stream's cursor is the id of the last frame it wrote, so listeners must see
// events in history order: one seen after a higher id is lost to a client that
// reconnects in between.
func TestListenersSeeEventsInHistoryOrder(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []uint64
	)

	c := New(func(e Event) {
		mu.Lock()
		seen = append(seen, e.HistoryID)
		mu.Unlock()
	})

	const writers, writes = 8, 500

	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			for i := range writes {
				c.SetTask(swarm.Task{ID: strconv.Itoa(w) + "-" + strconv.Itoa(i)})
			}
		})
	}
	wg.Wait()

	for i := 1; i < len(seen); i++ {
		if seen[i] < seen[i-1] {
			t.Fatalf("listener saw history id %d after %d", seen[i], seen[i-1])
		}
	}
}
