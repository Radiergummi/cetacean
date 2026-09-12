//go:build e2e

package e2e_test

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives what a client that has stopped reading — a laptop asleep
// with the dashboard open — costs everybody else. internal/api/sse's own tests
// substitute a writer that never blocks, so nothing else can see it. Reserves
// port 19025 (see README.md's reserved-ports table).

const slowClientPort = 19025

// stalledClient opens an SSE connection and then stops reading from it.
func stalledClient(t *testing.T, proc *sut.Process, path string) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, proc.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	request.Header.Set("Accept", "text/event-stream")

	response, err := proc.StreamClient().Do(request)
	if err != nil {
		t.Fatalf("open stalled client: %v", err)
	}

	t.Cleanup(func() { _ = response.Body.Close() })

	// Enough to know the handler is subscribed; then never read again.
	buffer := make([]byte, 1)
	if _, err := response.Body.Read(buffer); err != nil {
		t.Fatalf("stalled client never received anything: %v", err)
	}
}

// countingClient reports every event line it sees until the test ends.
func countingClient(t *testing.T, proc *sut.Process, path string) <-chan string {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, proc.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	request.Header.Set("Accept", "text/event-stream")

	response, err := proc.StreamClient().Do(request)
	if err != nil {
		t.Fatalf("open counting client: %v", err)
	}

	lines := make(chan string, 4096)

	go func() {
		defer close(lines)
		defer response.Body.Close()

		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data:") {
				continue
			}

			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
	}()

	return lines
}

// probeNames is what the healthy subscriber is measured by: the broadcaster
// batches a whole interval into one `data:` line, so counting frames would
// measure the wrong thing.
var probeNames = regexp.MustCompile(`slow-client-probe-\d{3}`)

// TestAStalledClientDoesNotStarveTheOthers: a connection nobody is reading
// must cost only itself.
func TestAStalledClientDoesNotStarveTheOthers(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       slowClientPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE": "none",
		},
	})

	// Opened first, so they are ahead of the healthy one in the client map.
	for range 4 {
		stalledClient(t, proc, "/configs")
	}

	healthy := countingClient(t, proc, "/configs")

	// More than the 64-event per-client buffer, so the stalled clients are
	// genuinely overflowed rather than merely slow.
	const events = 120

	created := make([]string, 0, events)

	ctx := context.Background()

	for i := range events {
		name := fmt.Sprintf("slow-client-probe-%03d", i)

		response, err := env.Docker.ConfigCreate(ctx, swarm.ConfigSpec{
			Annotations: swarm.Annotations{Name: name},
			Data:        []byte("probe\n"),
		})
		if err != nil {
			t.Fatalf("ConfigCreate %s: %v", name, err)
		}

		created = append(created, response.ID)
	}

	t.Cleanup(func() {
		for _, id := range created {
			_ = env.Docker.ConfigRemove(context.Background(), id)
		}
	})

	// Most, not all: the broadcaster may drop events for a client that cannot
	// keep up. A blocked fan-out delivers almost nothing.
	const want = events * 3 / 4

	seen := map[string]bool{}
	deadline := time.After(90 * time.Second)

	for len(seen) < want {
		select {
		case line, ok := <-healthy:
			if !ok {
				t.Fatalf(
					"the healthy subscriber's stream closed having seen %d of %d configs; "+
						"a stalled client took it down with it",
					len(seen), events,
				)
			}

			for _, name := range probeNames.FindAllString(line, -1) {
				seen[name] = true
			}

		case <-deadline:
			t.Fatalf(
				"the healthy subscriber saw %d of %d configs in 90s, want at least %d\n"+
					"four stalled clients are subscribed, so the fan-out is blocking on a "+
					"client that stopped reading instead of dropping its events",
				len(seen), events, want,
			)
		}
	}

	t.Logf("healthy subscriber saw %d of %d configs past four stalled clients", len(seen), events)
}
