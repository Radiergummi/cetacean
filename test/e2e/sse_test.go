//go:build e2e

package e2e_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// sseFrame is one event decoded off a text/event-stream response body: the
// "event:" line names it and the (possibly multi-line) "data:" lines carry
// its JSON payload, per the SSE wire format. A blank line terminates a frame.
type sseFrame struct {
	event string
	data  string

	// id is the last id: field seen on the stream, sticky across frames the
	// way EventSource's Last-Event-ID is: a server that repeats a value omits
	// the field rather than resending it.
	id string
}

// readSSEFrames scans r for blank-line-terminated SSE frames and sends each on
// frames, closing it once r is exhausted. done releases it from a send no one
// will receive: closing the response body does not unblock a blocked channel
// send, so the goroutine and the close it owes would outlive the test.
func readSSEFrames(r io.Reader, frames chan<- sseFrame, done <-chan struct{}) {
	defer close(frames)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var event, id string

	var data strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case line == "":
			if event != "" || data.Len() > 0 {
				select {
				case frames <- sseFrame{event: event, data: data.String(), id: id}:
				case <-done:
					return
				}
			}

			event = ""

			data.Reset()
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "id:"):
			id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}

			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

// sseServiceEvent mirrors the fields of sse.Event (internal/api/sse) this
// test cares about, plus the one field of the embedded swarm.Service the
// test needs to confirm the resource field genuinely carries the service
// object rather than an empty or unrelated payload.
type sseServiceEvent struct {
	Type     string `json:"type"`
	Action   string `json:"action"`
	Resource struct {
		Spec struct {
			Name string `json:"Name"`
		} `json:"Spec"`
	} `json:"resource"`
}

// serviceEventsIn decodes a frame's data as either a single event or a "batch"
// array and returns the events it carries. /services' stream is already
// server-side filtered, so this is a decode step, not a second filter.
func serviceEventsIn(f sseFrame) []sseServiceEvent {
	if f.event != "service" && f.event != "batch" {
		return nil
	}

	trimmed := strings.TrimSpace(f.data)
	if trimmed == "" {
		return nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var events []sseServiceEvent
		if err := json.Unmarshal([]byte(trimmed), &events); err != nil {
			return nil
		}

		return events
	}

	var event sseServiceEvent
	if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
		return nil
	}

	return []sseServiceEvent{event}
}

// TestServiceCreationReachesTheSSEStream proves the watcher -> cache ->
// broadcaster chain against a real Docker event stream. It cannot be
// exercised in-process without reimplementing the watcher.
func TestServiceCreationReachesTheSSEStream(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       19001,
		DockerHost: env.DockerHost,
		Env:        map[string]string{"CETACEAN_AUTH_MODE": "none"},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proc.BaseURL+"/services", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "text/event-stream")

	// StreamClient, not Client: Client's 30s timeout bounds the whole
	// response including the body, and DeployStack below can take longer than
	// that to converge on a cold engine. Under Client the stream would be torn
	// down mid-deploy and this would report a broken watcher chain.
	resp, err := proc.StreamClient().Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	frames := make(chan sseFrame, 16)

	done := make(chan struct{})
	defer close(done)

	go readSSEFrames(resp.Body, frames, done)

	type found struct {
		event   sseServiceEvent
		elapsed time.Duration
	}

	seen := make(chan found, 1)
	start := time.Now()

	// Reads concurrently with the deploy below, started before it, so the
	// event this looks for must arrive live rather than being read only
	// after the stack has already converged.
	go func() {
		for f := range frames {
			for _, event := range serviceEventsIn(f) {
				if strings.Contains(event.Resource.Spec.Name, "ssefixture") {
					seen <- found{event: event, elapsed: time.Since(start)}

					return
				}
			}
		}
	}()

	// Deploy after the stream is open and being read, so the event must
	// arrive live. DeployStack blocks until the service converges, which is
	// fine here: the reader goroutine above is draining the stream the
	// whole time it does.
	fixtures.DeployStack(t, env, "ssefixture", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})

	select {
	case result := <-seen:
		t.Logf(
			"SSE event for service %q observed %s after the stream opened "+
				"(includes DeployStack's own setup and convergence wait, "+
				"not just watcher latency)",
			result.event.Resource.Spec.Name, result.elapsed,
		)

		if result.event.Type != "service" {
			t.Errorf("event type = %q, want service", result.event.Type)
		}

		if result.event.Action != "create" {
			t.Errorf("event action = %q, want create", result.event.Action)
		}

	case <-ctx.Done():
		t.Errorf("no SSE event naming the new service within the timeout")
	}
}
