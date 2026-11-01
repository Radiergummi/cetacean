package audit

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileAppendsAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	f, err := OpenFile(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	if err := f.Write(Record{Event: "exec.start"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	rotated := path + ".1"
	if err := os.Rename(path, rotated); err != nil {
		t.Fatal(err)
	}
	if err := f.Reopen(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := f.Write(Record{Event: "exec.end"}); err != nil {
		t.Fatalf("write after reopen: %v", err)
	}

	old, _ := os.ReadFile(rotated)
	current, _ := os.ReadFile(path)
	if !strings.Contains(string(old), "exec.start") ||
		!strings.Contains(string(current), "exec.end") {
		t.Errorf("rotated=%q current=%q", old, current)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

type blockingWriter struct{ release chan struct{} }

func (b blockingWriter) Write(p []byte) (int, error) { <-b.release; return len(p), nil }
func (blockingWriter) Close() error                  { return nil }

func TestFileFailsClosedOnDeadline(t *testing.T) {
	w := blockingWriter{release: make(chan struct{})}
	f := newFile(func() (io.WriteCloser, error) { return w, nil }, 50*time.Millisecond)
	if err := f.reopen(); err != nil {
		t.Fatal(err)
	}

	if err := f.Write(Record{Event: "exec.start"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stuck write = %v, want ErrUnavailable", err)
	}
	// Still stuck: a second write waits no longer than its own deadline.
	if err := f.Write(Record{Event: "exec.start"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("second write = %v, want ErrUnavailable", err)
	}

	close(w.release)
	time.Sleep(20 * time.Millisecond)
	if err := f.Write(Record{Event: "exec.start"}); err != nil {
		t.Fatalf("write after recovery: %v", err)
	}
}

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }

// TestFileReopenAndCloseDontBlockOnStuckWrite pins that a write stuck inside a
// wedged io.Writer, long after its caller has received ErrUnavailable, cannot
// also wedge the SIGHUP recovery path or Close.
func TestFileReopenAndCloseDontBlockOnStuckWrite(t *testing.T) {
	stuck := blockingWriter{release: make(chan struct{})}
	t.Cleanup(func() { close(stuck.release) })

	var calls atomic.Int32
	open := func() (io.WriteCloser, error) {
		if calls.Add(1) == 1 {
			return stuck, nil
		}

		return discardWriteCloser{}, nil
	}

	f := newFile(open, 20*time.Millisecond)
	if err := f.reopen(); err != nil {
		t.Fatal(err)
	}

	writeDone := make(chan error, 1)
	go func() { writeDone <- f.Write(Record{Event: "exec.start"}) }()

	select {
	case err := <-writeDone:
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("write = %v, want ErrUnavailable", err)
		}
	case <-time.After(time.Second):
		t.Fatal("write did not return within the deadline")
	}

	// The write's goroutine is still stuck inside stuck.Write. Reopen and
	// Close must return promptly rather than wait for it.
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := f.Reopen(); err != nil {
			t.Errorf("reopen: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reopen/close blocked on the stuck write")
	}
}

type erroringWriter struct{}

func (erroringWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }
func (erroringWriter) Close() error              { return nil }

func TestFileFailsClosedOnError(t *testing.T) {
	f := newFile(func() (io.WriteCloser, error) { return erroringWriter{}, nil }, time.Second)
	if err := f.reopen(); err != nil {
		t.Fatal(err)
	}
	if err := f.Write(Record{Event: "exec.start"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("write = %v, want ErrUnavailable", err)
	}
}

type slowFile struct{ *os.File }

func (s slowFile) Write(p []byte) (int, error) {
	time.Sleep(5 * time.Millisecond)
	return s.File.Write(p)
}

func TestFileQueuesOverlappingWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	f := newFile(func() (io.WriteCloser, error) {
		w, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		return slowFile{w}, err
	}, time.Second)
	if err := f.reopen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })

	const n = 20
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			if err := f.Write(Record{Event: "exec.end"}); err != nil {
				t.Errorf("overlapping write = %v", err)
			}
		})
	}
	wg.Wait()

	b, _ := os.ReadFile(path)
	if got := strings.Count(string(b), "exec.end"); got != n {
		t.Errorf("%d records on disk, want %d", got, n)
	}
}

// closeTrackingWriter fails a write still running when it is closed, as a
// file does once its descriptor is gone.
type closeTrackingWriter struct {
	started, release chan struct{}
	closed           atomic.Bool
}

func (c *closeTrackingWriter) Write(p []byte) (int, error) {
	close(c.started)
	<-c.release
	if c.closed.Load() {
		return 0, os.ErrClosed
	}

	return len(p), nil
}

func (c *closeTrackingWriter) Close() error { c.closed.Store(true); return nil }

func TestReopenWaitsForTheWriteInFlight(t *testing.T) {
	old := &closeTrackingWriter{started: make(chan struct{}), release: make(chan struct{})}
	writers := []io.WriteCloser{old, discardWriteCloser{}}
	f := newFile(func() (io.WriteCloser, error) {
		w := writers[0]
		writers = writers[1:]
		return w, nil
	}, time.Second)
	if err := f.reopen(); err != nil {
		t.Fatal(err)
	}

	written := make(chan error, 1)
	go func() { written <- f.Write(Record{Event: "exec.start"}) }()
	<-old.started

	reopened := make(chan error, 1)
	go func() { reopened <- f.Reopen() }()

	time.Sleep(20 * time.Millisecond)
	close(old.release)

	if err := <-written; err != nil {
		t.Errorf("write across a reopen = %v, want it to land in the old file", err)
	}
	if err := <-reopened; err != nil || !old.closed.Load() {
		t.Errorf(
			"reopen = %v, old closed = %v; want the old file closed after",
			err,
			old.closed.Load(),
		)
	}
}

func TestCloseWaitsForTheWriteInFlight(t *testing.T) {
	w := &closeTrackingWriter{started: make(chan struct{}), release: make(chan struct{})}
	f := newFile(func() (io.WriteCloser, error) { return w, nil }, time.Second)
	if err := f.reopen(); err != nil {
		t.Fatal(err)
	}

	written := make(chan error, 1)
	go func() { written <- f.Write(Record{Event: "exec.end"}) }()
	<-w.started

	closed := make(chan error, 1)
	go func() { closed <- f.Close() }()

	time.Sleep(20 * time.Millisecond)
	close(w.release)

	if err := <-written; err != nil {
		t.Errorf("write across a close = %v, want it to land", err)
	}
	if err := <-closed; err != nil {
		t.Errorf("close = %v", err)
	}
}
