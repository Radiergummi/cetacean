//go:build e2e

package e2e_test

import (
	"bufio"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives what a week of use does to a process. A goroutine that
// outlives its connection is not a slow leak: it is one per browser tab per
// reconnect. It reserves port 19024 (see README.md's reserved-ports table).

const leakPort = 19024

// acceptableGoroutineDrift is not zero — runtime workers and pooled
// connections move the count on their own — but is small enough that one
// goroutine left per closed connection fails a round.
const acceptableGoroutineDrift = 20

// goroutineCount reads the total from the pprof goroutine profile.
func goroutineCount(t *testing.T, proc *sut.Process) int {
	t.Helper()

	response, err := proc.Client().Get( //nolint:noctx // test probe
		proc.BaseURL + "/debug/pprof/goroutine?debug=1")
	if err != nil {
		t.Fatalf("GET /debug/pprof/goroutine: %v", err)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf(
			"GET /debug/pprof/goroutine: status %d (is CETACEAN_PPROF set?)",
			response.StatusCode,
		)
	}

	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()

		// "goroutine profile: total 42"
		if rest, found := strings.CutPrefix(line, "goroutine profile: total "); found {
			total, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				t.Fatalf("goroutine profile total %q: %v", rest, err)
			}

			return total
		}
	}

	t.Fatal("goroutine profile carried no total line")

	return 0
}

// settleGoroutines waits for a closed round to be reaped: returning from the
// handler is not the instant the goroutine is gone.
func settleGoroutines(t *testing.T, proc *sut.Process, want int) int {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)

	count := goroutineCount(t, proc)

	for time.Now().Before(deadline) {
		if count <= want {
			return count
		}

		time.Sleep(500 * time.Millisecond)

		count = goroutineCount(t, proc)
	}

	return count
}

// baselineGoroutines waits for the count to stop moving, so assertions are
// measured against a figure the process has settled on.
func baselineGoroutines(t *testing.T, proc *sut.Process) int {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	previous := -1

	for time.Now().Before(deadline) {
		count := goroutineCount(t, proc)

		if count == previous {
			return count
		}

		previous = count

		time.Sleep(time.Second)
	}

	return previous
}

// openAndClose opens count streaming connections, reads enough from each to
// know the handler is running, then closes them all.
func openAndClose(t *testing.T, proc *sut.Process, path string, count int) {
	t.Helper()

	var wg sync.WaitGroup

	for range count {
		wg.Add(1)

		go func() {
			defer wg.Done()

			request, err := http.NewRequest(http.MethodGet, proc.BaseURL+path, nil)
			if err != nil {
				return
			}

			request.Header.Set("Accept", "text/event-stream")

			response, err := proc.StreamClient().Do(request)
			if err != nil {
				return
			}

			// One chunk proves the handler is past setup; then hang up.
			buffer := make([]byte, 1)
			_, _ = response.Body.Read(buffer)
			_ = response.Body.Close()
		}()
	}

	wg.Wait()
}

// TestStreamingConnectionsDoNotLeakGoroutines requires the process to end up
// where it started.
func TestStreamingConnectionsDoNotLeakGoroutines(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	// Deliberately not the baseline: shop_flaky crash-loops by design, and
	// each restart schedules a task re-read on its own goroutine.

	proc := sut.Start(t, sut.Config{
		Port:       leakPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE": "none",
			"CETACEAN_PPROF":     "true",
		},
	})

	// Warm up first: the pools the first connection of each kind builds would
	// otherwise read as a leak.
	openAndClose(t, proc, "/services", 5)
	openAndClose(t, proc, "/events", 5)

	baseline := baselineGoroutines(t, proc)
	t.Logf("baseline goroutines: %d", baseline)

	rounds := []struct {
		name string
		path string
	}{
		{name: "per-resource streams", path: "/services"},
		{name: "the legacy events stream", path: "/events"},
		{name: "detail streams", path: "/nodes"},
	}

	for _, round := range rounds {
		t.Run(round.name, func(t *testing.T) {
			openAndClose(t, proc, round.path, 40)

			count := settleGoroutines(t, proc, baseline+acceptableGoroutineDrift)

			if count > baseline+acceptableGoroutineDrift {
				t.Errorf(
					"goroutines went from %d to %d after 40 connections to %s came and went; "+
						"a connection that leaves a goroutine behind leaks one per browser tab "+
						"per reconnect",
					baseline, count, round.path,
				)
			}
		})
	}

	final := settleGoroutines(t, proc, baseline+acceptableGoroutineDrift)
	t.Logf("goroutines after %d connections: %d (baseline %d)", 120, final, baseline)

	if final > baseline+acceptableGoroutineDrift {
		t.Errorf(
			"after 120 connections came and went the process holds %d goroutines, up from %d",
			final, baseline,
		)
	}
}

// TestLogTailsDoNotLeakGoroutines holds a Docker log stream open behind the
// SSE connection, so hanging up has to unwind both.
func TestLogTailsDoNotLeakGoroutines(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	// A quiet stack of its own, for the reason the case above gives.
	// DeployStack suffixes the name, so the service is named from its return.
	stack := fixtures.DeployStack(t, env, "leakprobe", []fixtures.ServiceSpec{
		{Name: "quiet", Replicas: 1, Command: []string{"sleep infinity"}},
	})

	proc := sut.Start(t, sut.Config{
		Port:       leakPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE": "none",
			"CETACEAN_PPROF":     "true",
		},
	})

	serviceID := readListFindID(t, proc, "/services", stack+"_quiet")
	if serviceID == "" {
		t.Fatal("the probe service is not in the listing")
	}

	logPath := fmt.Sprintf("/services/%s/logs?follow=true", serviceID)

	openAndClose(t, proc, logPath, 5)

	baseline := baselineGoroutines(t, proc)
	t.Logf("baseline goroutines: %d", baseline)

	openAndClose(t, proc, logPath, 30)

	count := settleGoroutines(t, proc, baseline+acceptableGoroutineDrift)

	if count > baseline+acceptableGoroutineDrift {
		t.Errorf(
			"goroutines went from %d to %d after 30 log tails came and went; "+
				"the Docker log stream behind each one is not being unwound",
			baseline, count,
		)
	}
}
