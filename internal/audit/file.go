package audit

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const writeDeadline = 2 * time.Second

// File appends records to a file and fails closed. Regular files take no
// deadlines, so a write runs aside and is abandoned at the deadline. Waiting
// for an earlier write counts against the same deadline, so overlapping writes
// queue while one that is stuck makes the rest fail.
type File struct {
	open     func() (io.WriteCloser, error)
	deadline time.Duration

	mu   sync.Mutex
	w    io.WriteCloser
	busy chan struct{}
}

func OpenFile(path string) (*File, error) {
	f := newFile(func() (io.WriteCloser, error) {
		return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	}, writeDeadline)

	if err := f.reopen(); err != nil {
		return nil, err
	}

	return f, nil
}

func newFile(open func() (io.WriteCloser, error), deadline time.Duration) *File {
	return &File{open: open, deadline: deadline, busy: make(chan struct{}, 1)}
}

// Reopen swaps in a freshly opened file, for rotation on SIGHUP.
func (f *File) Reopen() error { return f.reopen() }

func (f *File) reopen() error {
	w, err := f.open()
	if err != nil {
		return err
	}

	f.mu.Lock()
	old := f.w
	f.w = w
	f.mu.Unlock()

	if old != nil {
		return f.closeIdle(old)
	}

	return nil
}

// closeIdle closes w once no write holds it: a write reads the file under the
// lock but writes outside it. Past the deadline that write has already failed,
// so w is closed regardless.
func (f *File) closeIdle(w io.Closer) error {
	timer := time.NewTimer(f.deadline)
	defer timer.Stop()

	select {
	case f.busy <- struct{}{}:
		defer func() { <-f.busy }()
	case <-timer.C:
	}

	return w.Close()
}

func (f *File) Write(r Record) error {
	b, err := line(r)
	if err != nil {
		return err
	}

	timer := time.NewTimer(f.deadline)
	defer timer.Stop()

	select {
	case f.busy <- struct{}{}:
	case <-timer.C:
		return fmt.Errorf("%w: a previous write did not complete within %s",
			ErrUnavailable, f.deadline)
	}

	done := make(chan error, 1)
	go func() {
		defer func() { <-f.busy }()
		f.mu.Lock()
		w := f.w
		f.mu.Unlock()
		_, err := w.Write(b)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}

		return nil
	case <-timer.C:
		return &AbandonedError{Landed: done, deadline: f.deadline}
	}
}

// AbandonedError is a write given up at its deadline, which goes on aside and
// may still land; Landed yields its result once it ends.
type AbandonedError struct {
	Landed   <-chan error
	deadline time.Duration
}

func (e *AbandonedError) Error() string {
	return fmt.Sprintf("%s: write exceeded %s", ErrUnavailable, e.deadline)
}

func (e *AbandonedError) Unwrap() error { return ErrUnavailable }

func (f *File) Close() error {
	f.mu.Lock()
	w := f.w
	f.mu.Unlock()

	return f.closeIdle(w)
}
