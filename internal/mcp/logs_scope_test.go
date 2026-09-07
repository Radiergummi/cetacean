package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/docker"
)

// failOnceStreamer fails the first read and serves frames thereafter, so a
// fan-out test can assert partial success rather than all-or-nothing.
type failOnceStreamer struct {
	frames []byte
	failed bool
	mu     sync.Mutex
}

func (f *failOnceStreamer) Logs(
	_ context.Context,
	_ docker.LogKind,
	_, _ string,
	_ bool,
	_, _ string,
) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.failed {
		f.failed = true

		return nil, errors.New("docker unavailable")
	}

	return io.NopCloser(bytes.NewReader(f.frames)), nil
}

// The saving this task exists for: one call instead of one per service, with
// the merge done server-side.
func TestClusterScopeReadsEveryService(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"web", "api"} {
		c.SetService(swarm.Service{
			ID:   name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	// One line per service, merged into one response.
	if len(got.Lines) != 2 {
		t.Fatalf("lines = %d, want 2 (one per service)", len(got.Lines))
	}
}

// Every line must say which service it came from, or a merged read is
// unreadable.
func TestClusterScopeAttributesEachLine(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if got.Lines[0].Attrs["serviceName"] != "web" {
		t.Errorf("serviceName = %v, want web", got.Lines[0].Attrs["serviceName"])
	}
}

// grep-the-cluster, done server-side: the caller pays for matches, not for
// every line of every service.
func TestContainsFiltersServerSide(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	streamer := &fakeLogStreamer{
		frames: append(
			buildLogFrame(1, "2026-09-05T14:00:00.000000000Z connection refused\n"),
			buildLogFrame(1, "2026-09-05T14:00:01.000000000Z all good\n")...,
		),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(
		context.Background(), "cluster", "",
		logOptions{tail: 10, contains: "refused"},
	)
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Fatalf("lines = %d, want 1 match", len(got.Lines))
	}
	if !strings.Contains(got.Lines[0].Message, "refused") {
		t.Errorf("kept the wrong line: %q", got.Lines[0].Message)
	}
}

// A stack scope reads only its own members.
func TestStackScopeReadsOnlyItsMembers(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
			Name:   "demo_web",
			Labels: map[string]string{"com.docker.stack.namespace": "demo"},
		}},
	})
	c.SetService(swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
			Name:   "other_api",
			Labels: map[string]string{"com.docker.stack.namespace": "other"},
		}},
	})

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(context.Background(), "stack", "demo", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Fatalf("lines = %d, want 1 (only the demo stack's service)", len(got.Lines))
	}
	if got.Lines[0].Attrs["serviceName"] != "demo_web" {
		t.Errorf("read the wrong service: %v", got.Lines[0].Attrs["serviceName"])
	}
}

// One unreadable service must not fail the whole read: a cluster-wide grep
// that dies on the first broken service is worse than no tool.
func TestScopedReadSurvivesOneFailingService(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"web", "api"} {
		c.SetService(swarm.Service{
			ID:   name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	srv := newLogTestServer(t, c, &failOnceStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	})

	got, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Errorf("lines = %d, want the one service that worked", len(got.Lines))
	}
	if len(got.Errors) != 1 {
		t.Errorf("errors = %d, want the one that failed reported", len(got.Errors))
	}
}

// The fan-out cap is disclosed in the tool description, but a model reasons
// over the payload: a cluster-wide grep that read a quarter of the services
// and matched nothing is otherwise indistinguishable from a clean cluster.
func TestClusterScopeSaysWhenItDidNotReadEverything(t *testing.T) {
	c := cache.New(nil)

	const services = maxScopedServices + 12

	for i := range services {
		name := fmt.Sprintf("svc-%02d", i)
		c.SetService(swarm.Service{
			ID:   name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 500})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if got.Note == "" {
		t.Fatal("note is empty; a truncated read must say so in the payload")
	}

	for _, want := range []string{
		strconv.Itoa(maxScopedServices),
		strconv.Itoa(services),
		strconv.Itoa(services - maxScopedServices),
	} {
		if !strings.Contains(got.Note, want) {
			t.Errorf("note = %q, want it to mention %s", got.Note, want)
		}
	}

	// The cap itself still holds — the note reports it, it does not lift it.
	if len(got.Lines) != maxScopedServices {
		t.Errorf(
			"lines = %d, want one per service actually read (%d)",
			len(got.Lines),
			maxScopedServices,
		)
	}
}

// And a read that covered its whole scope must not claim otherwise.
func TestClusterScopeIsSilentWhenItReadEverything(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"web", "api"} {
		c.SetService(swarm.Service{
			ID:   name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if got.Note != "" {
		t.Errorf("note = %q, want none: every service in scope was read", got.Note)
	}
}

// Errors is documented as always an array. A single-service read has no
// failures to report and must still answer with one rather than omitting the
// field — the promise is broken either way round.
func TestLogErrorsMarshalAsAnArrayWhenEmpty(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	resp, err := srv.readLogsImpl(
		context.Background(),
		docker.ServiceLog,
		"svc1",
		logOptions{tail: 10},
	)
	if err != nil {
		t.Fatalf("readLogsImpl: %v", err)
	}

	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if !bytes.Contains(encoded, []byte(`"errors":[]`)) {
		t.Errorf("payload = %s, want an empty errors array", encoded)
	}
}

// perServiceStreamer answers each service with its own frames, and records the
// `since` it was called with, so a resume can be inspected per service.
type perServiceStreamer struct {
	mu     sync.Mutex
	frames map[string][]byte
	since  map[string]string
}

func (p *perServiceStreamer) Logs(
	_ context.Context,
	_ docker.LogKind,
	id, _ string,
	_ bool,
	since, _ string,
) (io.ReadCloser, error) {
	p.mu.Lock()
	if p.since == nil {
		p.since = map[string]string{}
	}
	p.since[id] = since
	frames := p.frames[id]
	p.mu.Unlock()

	return io.NopCloser(bytes.NewReader(frames)), nil
}

// A fan-out mints one cursor for many independent streams. Taken as a single
// newest timestamp it is whichever service ran ahead, and logs.FilterSince is a
// flat cut — so a line another service stamps earlier but Docker hands over
// late is discarded on the resume and never returned by any later call. The
// timestamp spread across 25 services is far wider than across one service's
// replicas, which is why the single-service cursor logic does not carry over.
func TestScopedReadResumesEachServiceFromItsOwnPosition(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"ahead", "behind"} {
		c.SetService(swarm.Service{
			ID:   name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	streamer := &perServiceStreamer{
		frames: map[string][]byte{
			// Only the service that is ahead has flushed anything yet.
			"ahead": buildLogFrame(1, "2026-09-05T14:00:10.000000000Z ahead-line\n"),
		},
	}
	srv := newLogTestServer(t, c, streamer)

	first, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}
	if len(first.Lines) != 1 {
		t.Fatalf("lines = %d, want the one line flushed so far", len(first.Lines))
	}
	if first.Cursor == "" {
		t.Fatal("cursor is empty after a read that returned a line")
	}

	// The line the other service had all along, stamped five seconds earlier
	// than the one already delivered, now arrives.
	streamer.mu.Lock()
	streamer.frames["behind"] = buildLogFrame(1, "2026-09-05T14:00:05.000000000Z behind-line\n")
	streamer.mu.Unlock()

	second, err := srv.readScopedLogs(
		context.Background(), "cluster", "",
		logOptions{tail: 10, since: first.Cursor},
	)
	if err != nil {
		t.Fatalf("readScopedLogs (resume): %v", err)
	}

	var messages []string
	for _, line := range second.Lines {
		messages = append(messages, line.Message)
	}

	if !slices.Contains(messages, "behind-line") {
		t.Errorf("lines = %v, want the late line from the service that was behind", messages)
	}
	if slices.Contains(messages, "ahead-line") {
		t.Errorf("lines = %v, want no repeat of a line already delivered", messages)
	}

	// Each service was resumed from its own position, not from a shared one.
	streamer.mu.Lock()
	defer streamer.mu.Unlock()

	if streamer.since["ahead"] == "" {
		t.Error("the service that was ahead resumed from no position at all")
	}
	if streamer.since["behind"] != "" {
		t.Errorf(
			"the service that was behind resumed from %q; it had delivered nothing",
			streamer.since["behind"],
		)
	}
}

// A caller's own `since` is a plain timestamp and still applies to every
// service, and a cursor that is not one of ours must not be mistaken for one.
func TestScopedCursorRoundTripsAndIgnoresPlainTimestamps(t *testing.T) {
	positions := map[string]string{
		"svc1": "2026-09-05T14:00:05.000000000Z",
		"svc2": "2026-09-05T14:00:10.000000000Z",
	}

	encoded := encodeScopedCursor(positions)
	if encoded == encodeScopedCursor(nil) {
		t.Fatal("a populated cursor encoded the same as an empty one")
	}

	// Stable: the same positions must always encode identically.
	if encoded != encodeScopedCursor(positions) {
		t.Error("the same positions encoded differently on a second call")
	}

	decoded, ok := decodeScopedCursor(encoded)
	if !ok {
		t.Fatal("a cursor we produced was not recognised")
	}
	if !maps.Equal(decoded, positions) {
		t.Errorf("decoded = %v, want %v", decoded, positions)
	}

	for _, plain := range []string{
		"",
		"2026-09-05T14:00:05.000000000Z",
		"5m",
		"cs1:not-base64!!",
	} {
		if _, ok := decodeScopedCursor(plain); ok {
			t.Errorf("%q was read as a per-service cursor", plain)
		}
	}
}
