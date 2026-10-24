package docker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

// A change made while a sync lists the cluster is in neither the lists nor a
// stream subscribed after them, unless the stream starts before the sync did.
func TestEachStreamStartsBeforeTheSyncPrecedingIt(t *testing.T) {
	mc := newMockClient()

	var (
		mu     sync.Mutex
		starts []time.Time
	)
	mc.fullSyncFn = func(int64) (cache.FullSyncData, error) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()

		return cache.FullSyncData{}, nil
	}

	w := NewWatcher(mc, cache.New(nil), "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Two stream ends: the startup sync and one reconnect sync each precede a
	// subscription.
	mc.errCh <- errors.New("stream ended")
	mc.errCh <- errors.New("stream ended again")

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()

	deadline := time.After(10 * time.Second)
	for {
		mc.mu.Lock()
		subscribed := len(mc.eventsSince)
		mc.mu.Unlock()

		if subscribed >= 2 {
			break
		}

		select {
		case <-deadline:
			t.Fatal("the watcher never resubscribed")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for i := range 2 {
		since := mc.eventsSince[i]
		if since.IsZero() || !since.Before(starts[i]) {
			t.Errorf("stream %d starts at %v, want before its sync at %v", i, since, starts[i])
		}
	}
}
