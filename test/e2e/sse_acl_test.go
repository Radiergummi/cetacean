//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file closes the two SSE contracts nothing in the repository observed.
// Reserves port 19013 (see README.md's reserved-ports table).
//
//   - `GET /events` ACL filtering, the read sweep's one remaining gap. That
//     lane excused it correctly: its filtering runs inside aclMatchWrap on the
//     streaming path, and a plain GET with a JSON Accept header never reaches
//     it. Observing it needs a client that holds the stream open while the
//     cluster changes underneath it.
//   - The broadcaster's connection cap. README.md's Deferred section records
//     that no unit test pins it either — internal/api/sse's own tests
//     substitute a noopErrorWriter rather than driving a real client past
//     MaxClients — so until now nothing anywhere established that a client
//     arriving at a full broadcaster is told to come back rather than left
//     hanging.

const sseACLPort = 19013

// sseACLPolicy gives one persona everything and the other a read grant on
// services alone. readSweepPolicy cannot serve here: every one of its four
// personas holds a blanket read, which its own comment notes, so none of them
// can show an event being withheld.
const sseACLPolicy = `grants:
  - resources: ["*"]
    audience: ["group:ops"]
    permissions: ["read", "write"]

  - resources: ["service:*"]
    audience: ["group:svconly"]
    permissions: ["read"]
`

var (
	sseOps     = readPersona{name: "ops", user: "ops@example.com", groups: "ops"}
	sseSvcOnly = readPersona{
		name:   "svconly",
		user:   "svconly@example.com",
		groups: "svconly",
	}
)

// sseAnyEvent is the wire shape of internal/api/sse.Event. There is
// deliberately no name field on it — identifying a resource means matching the
// id, which is why the cases below capture engine ids before they subscribe.
type sseAnyEvent struct {
	Type   string `json:"type"`
	Action string `json:"action"`
	ID     string `json:"id"`
}

// eventsInFrame decodes a frame's data as a single event or a batch array.
// Unlike sse_test.go's serviceEventsIn it filters on nothing: these cases are
// about which events a subscriber is and is not given, so discarding any of
// them here would be answering the question the test is asking.
func eventsInFrame(frame sseFrame) []sseAnyEvent {
	trimmed := strings.TrimSpace(frame.data)
	if trimmed == "" {
		return nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var events []sseAnyEvent
		if err := json.Unmarshal([]byte(trimmed), &events); err != nil {
			return nil
		}

		return events
	}

	var event sseAnyEvent
	if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
		return nil
	}

	return []sseAnyEvent{event}
}

// eventRecorder holds one subscriber's stream open and accumulates everything
// it was given, so a case can ask both what arrived and what never did.
type eventRecorder struct {
	persona readPersona

	mu     sync.Mutex
	events []sseAnyEvent
}

func (r *eventRecorder) record(events []sseAnyEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, events...)
}

func (r *eventRecorder) seen(eventType, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, event := range r.events {
		if event.Type == eventType && event.ID == id {
			return true
		}
	}

	return false
}

func (r *eventRecorder) seenType(eventType string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, event := range r.events {
		if event.Type == eventType {
			return true
		}
	}

	return false
}

// subscribeEvents opens GET /events as persona and drains it into a recorder
// for the life of ctx.
func subscribeEvents(
	t *testing.T,
	ctx context.Context,
	proc *sut.Process,
	persona readPersona,
) *eventRecorder {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proc.BaseURL+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	// Without this the negotiation switch falls to its default case and
	// serves the SPA, which is exactly why a request/response sweep cannot
	// reach the filtering under test.
	req.Header.Set("Accept", "text/event-stream")
	applyPersona(req, persona)

	resp, err := proc.StreamClient().Do(req)
	if err != nil {
		t.Fatalf("open /events as %s: %v", persona.name, err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("GET /events as %s: status = %d, want 200", persona.name, resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		resp.Body.Close()
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	recorder := &eventRecorder{persona: persona}

	frames := make(chan sseFrame, 64)
	done := make(chan struct{})

	go readSSEFrames(resp.Body, frames, done)

	go func() {
		for frame := range frames {
			recorder.record(eventsInFrame(frame))
		}
	}()

	t.Cleanup(func() {
		close(done)
		resp.Body.Close()
	})

	return recorder
}

// awaitEvent waits for a recorder to have seen one specific resource's event.
func awaitEvent(t *testing.T, recorder *eventRecorder, eventType, id, what string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)

	for {
		if recorder.seen(eventType, id) {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("%s never reached %s's stream (%s)", what, recorder.persona.name, id)
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// TestEventsStreamAppliesTheACL drives the SSE authorization boundary: a
// subscriber must be given events only for resources it could also read over
// HTTP.
//
// The negative half is the point, and a negative alone proves nothing — a
// stream that delivered nothing at all would satisfy it. So the case pins the
// withheld event from two sides: the fully granted persona must receive it,
// which establishes it was broadcast at all, and the narrow persona must
// receive a *different* event from the same window, which establishes its
// stream was live and delivering throughout.
func TestEventsStreamAppliesTheACL(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startSSEACL(t, env)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ops := subscribeEvents(t, ctx, proc, sseOps)
	narrow := subscribeEvents(t, ctx, proc, sseSvcOnly)

	// Created after both streams are open, so both events must arrive live.
	configName := sweepName("sse-acl-config")
	configID := engineConfig(t, env, configName, nil)

	service := deployThrowawayService(t, env, "sseacl")

	svc, _, err := env.Docker.ServiceInspectWithRaw(
		context.Background(), service, swarm.ServiceInspectOptions{},
	)
	if err != nil {
		t.Fatalf("ServiceInspectWithRaw %s: %v", service, err)
	}

	// The config was created first, so by the time either stream has the
	// service event the config event's own window has long passed. The
	// watcher's inspect pool makes that an argument about elapsed time rather
	// than about ordering, which is why the grace period below exists too.
	awaitEvent(t, ops, "config", configID, "the config create event")
	awaitEvent(t, ops, "service", svc.ID, "the service create event")
	awaitEvent(t, narrow, "service", svc.ID, "the service create event")

	time.Sleep(3 * time.Second)

	if narrow.seen("config", configID) {
		t.Errorf(
			"the svconly persona was given a config event for %s; its only grant is "+
				"service:*, and aclMatchWrap is meant to withhold every event it holds "+
				"no read grant for",
			configName,
		)
	}

	// Stronger than the id check alone: a grant on service:* must not leak
	// any config event, not merely this one.
	if narrow.seenType("config") {
		t.Error("the svconly persona was given config events despite holding no config grant")
	}
}

// TestSSEConnectionCapRefusesWithRetryAfter drives the broadcaster to
// MaxClients and asserts the next subscriber is refused in a way it can act
// on. A cap that dropped the connection, or hung, or answered 503 would leave
// a client with no way to distinguish "come back shortly" from "this endpoint
// is broken" — which is why the contract is a 429 with Retry-After rather than
// any refusal at all.
func TestSSEConnectionCapRefusesWithRetryAfter(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startSSEACL(t, env)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// ServeSSE registers a client before writing the response head, so a
	// request whose headers have arrived is a client the broadcaster is
	// already counting. Opening these serially therefore reaches exactly
	// MaxClients with no race to reason about.
	for opened := range sse.MaxClients {
		req, err := http.NewRequestWithContext(
			ctx, http.MethodGet, proc.BaseURL+"/events", nil,
		)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		req.Header.Set("Accept", "text/event-stream")
		applyPersona(req, sseOps)

		resp, err := proc.StreamClient().Do(req)
		if err != nil {
			t.Fatalf("open stream %d of %d: %v", opened+1, sse.MaxClients, err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf(
				"stream %d of %d was refused with %d; the broadcaster ran out of room "+
					"before MaxClients",
				opened+1, sse.MaxClients, resp.StatusCode,
			)
		}

		t.Cleanup(func() { resp.Body.Close() })
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proc.BaseURL+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	applyPersona(req, sseOps)

	outcome := send(t, proc, req)

	if outcome.status != http.StatusTooManyRequests {
		t.Fatalf(
			"subscriber %d: status = %d, want 429; body: %s",
			sse.MaxClients+1, outcome.status, outcome.body,
		)
	}

	if got := outcome.header.Get("Retry-After"); got != "5" {
		t.Errorf("Retry-After = %q, want 5", got)
	}

	if !strings.Contains(outcome.body, "SSE001") {
		t.Errorf("the refusal does not name SSE001: %s", outcome.body)
	}

	// RFC 9457, like every other error this API produces. A refusal in some
	// other shape is one a client's existing error handling cannot read.
	if ct := outcome.header.Get("Content-Type"); !strings.HasPrefix(
		ct, "application/problem+json",
	) {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
}

// startSSEACL brings up a headers-auth SUT with sseACLPolicy.
func startSSEACL(t *testing.T, env *harness.Env) *sut.Process {
	t.Helper()

	policy := filepath.Join(t.TempDir(), "acl.yaml")
	if err := os.WriteFile(policy, []byte(sseACLPolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	return sut.Start(t, sut.Config{
		Port:       sseACLPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":            "headers",
			"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
			"CETACEAN_AUTH_HEADERS_GROUPS":  "X-Auth-Groups",
			"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
			"CETACEAN_ACL_POLICY_FILE":      policy,
			"CETACEAN_OPERATIONS_LEVEL":     "2",

			// The default 100ms batch window would coalesce the config and
			// service events these cases distinguish into one frame. They
			// decode a batch array either way, but a shorter window keeps the
			// two arrivals separable in time, which is what the grace period
			// below the awaits reasons about.
			"CETACEAN_SSE_BATCH_INTERVAL": "20ms",
		},
	})
}
