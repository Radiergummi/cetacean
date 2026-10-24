package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Everything the stream applied since the last periodic sync would otherwise
// be lost to the next start, which loads the snapshot before it syncs.
func TestRunWritesASnapshotWhenItStops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")

	w := NewWatcher(newMockClient(), cache.New(nil), path)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()

	<-w.Ready()
	waitFor(t, "the startup snapshot", func() bool {
		_, err := os.Stat(path)
		return err == nil
	})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("no snapshot written at shutdown: %v", err)
	}
}

// A cache no sync ever reached holds nothing newer than the snapshot it was
// loaded from, so stopping leaves that snapshot as it is.
func TestRunWritesNoSnapshotWithoutASync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")

	mc := newMockClient()
	for _, name := range swarmResources {
		mc.listErrors[name] = os.ErrDeadlineExceeded
	}
	w := NewWatcher(mc, cache.New(nil), path)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()

	waitFor(t, "the stream to open", func() bool { return w.loopRunning.Load() })
	cancel()
	<-done

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stat = %v, want no snapshot", err)
	}
}
