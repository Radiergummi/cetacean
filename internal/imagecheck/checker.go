package imagecheck

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

const (
	checkInterval  = 1 * time.Hour
	firstRetry     = 5 * time.Minute
	requestTimeout = 15 * time.Second
	scanInterval   = 1 * time.Minute

	// Bounds on what one scan asks of the registries, however many images.
	maxConcurrent  = 4
	requestSpacing = 250 * time.Millisecond
)

// Result is what a registry last said a tag names.
type Result struct {
	Digest    string
	CheckedAt time.Time

	// Since is when the registry was first seen to name Digest.
	Since time.Time
}

type entry struct {
	result   Result
	known    bool
	nextAt   time.Time
	failures int
}

// Checker resolves the tag of every pinned service image on a schedule and
// keeps the answers. Each distinct reference is checked once, however many
// services run it.
type Checker struct {
	services func() []swarm.Service
	client   *Client
	now      func() time.Time
	spacing  time.Duration

	mu         sync.RWMutex
	entries    map[string]*entry
	generation atomic.Uint64
}

// New returns a checker over the services list returns.
func New(list func() []swarm.Service) *Checker {
	return &Checker{
		services: list,
		client:   NewClient(&http.Client{Timeout: requestTimeout, Transport: guardedTransport()}),
		now:      time.Now,
		spacing:  requestSpacing,
		entries:  map[string]*entry{},
	}
}

// LatestDigest reports what the registry last said image's tag names. A nil
// checker knows nothing, so a disabled check reads like an unanswered one.
func (c *Checker) LatestDigest(image string) (Result, bool) {
	if c == nil {
		return Result{}, false
	}

	ref, _, ok := ParsePinned(image)
	if !ok {
		return Result{}, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.entries[ref.Key()]
	if !ok || !e.known {
		return Result{}, false
	}

	return e.result, true
}

// Generation advances whenever an answer LatestDigest gives changes, which
// anything caching a response built from those answers must key on.
func (c *Checker) Generation() uint64 {
	if c == nil {
		return 0
	}

	return c.generation.Load()
}

// RunAfter scans once ready closes, then every scanInterval until ctx ends.
// A scan only asks about references that are new or due.
func (c *Checker) RunAfter(ctx context.Context, ready <-chan struct{}) {
	if ready != nil {
		select {
		case <-ready:
		case <-ctx.Done():
			return
		}
	}

	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for {
		c.scan(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// scan forgets references no service uses any more and checks those due.
func (c *Checker) scan(ctx context.Context) {
	refs := map[string]Reference{}

	for _, svc := range c.services() {
		if svc.Spec.TaskTemplate.ContainerSpec == nil {
			continue
		}

		if ref, _, ok := ParsePinned(svc.Spec.TaskTemplate.ContainerSpec.Image); ok {
			refs[ref.Key()] = ref
		}
	}

	now := c.now()

	var due []Reference

	c.mu.Lock()

	for key, e := range c.entries {
		if _, used := refs[key]; !used {
			delete(c.entries, key)

			if e.known {
				c.generation.Add(1)
			}
		}
	}

	for key, ref := range refs {
		e, ok := c.entries[key]
		if !ok {
			e = &entry{}
			c.entries[key] = e
		}

		if !now.Before(e.nextAt) {
			due = append(due, ref)
		}
	}

	c.mu.Unlock()

	c.checkAll(ctx, due)
}

func (c *Checker) checkAll(ctx context.Context, refs []Reference) {
	slots := make(chan struct{}, maxConcurrent)

	var wg sync.WaitGroup

	for i, ref := range refs {
		if i > 0 && c.spacing > 0 {
			select {
			case <-ctx.Done():
				wg.Wait()

				return
			case <-time.After(c.spacing):
			}
		}

		select {
		case <-ctx.Done():
			wg.Wait()

			return
		case slots <- struct{}{}:
		}

		wg.Go(func() {
			defer func() { <-slots }()

			requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
			defer cancel()

			digest, err := c.client.Resolve(requestCtx, ref)
			c.record(ref, digest, err)
		})
	}

	wg.Wait()
}

// record stores an answer. A private or unknown tag forgets what was known,
// since nothing can be said about it any more; any other failure keeps the
// last answer and retries sooner, backing off to the normal interval.
func (c *Checker) record(ref Reference, digest string, err error) {
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[ref.Key()]
	if !ok {
		return // no service uses it any more
	}

	switch {
	case err == nil:
		if !e.known || e.result.Digest != digest {
			e.result.Since = now
			c.generation.Add(1)
		}

		e.result.Digest, e.result.CheckedAt, e.known = digest, now, true
		e.failures = 0
		e.nextAt = now.Add(checkInterval)

	case errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrNotFound):
		slog.Debug("image check skipped", "image", ref.Key(), "reason", err)

		if e.known {
			c.generation.Add(1)
		}

		e.known, e.result = false, Result{}
		e.failures = 0
		e.nextAt = now.Add(checkInterval)

	default:
		e.failures++

		if e.failures == 1 {
			slog.Warn("image check failed", "image", ref.Key(), "error", err)
		} else {
			slog.Debug(
				"image check failed",
				"image",
				ref.Key(),
				"error",
				err,
				"failures",
				e.failures,
			)
		}

		e.nextAt = now.Add(backoff(e.failures))
	}
}

func backoff(failures int) time.Duration {
	wait := firstRetry
	for range failures - 1 {
		wait *= 2
		if wait >= checkInterval {
			return checkInterval
		}
	}

	return wait
}
