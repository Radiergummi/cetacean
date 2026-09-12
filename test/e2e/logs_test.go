//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives the two log endpoints — GET /services/{id}/logs and
// GET /tasks/{id}/logs — against containers whose output this suite wrote
// itself: frame decoding, the stream and cursor parameters and their refusals,
// the SSE tail and its resume, and the 128-stream cap. It reserves port 19015.

const logsPort = 19015

// maxLogSSEConnsRef must match the unexported maxLogSSEConns in
// internal/api/handlers.go, which is also the limit api/openapi.yaml and
// docs/api.md publish. There is no exported way to read it from here, so a
// change to it fails this lane rather than going unnoticed.
const maxLogSSEConnsRef = 128

// burstLines is how many stdout/stderr pairs the burst fixture writes. It is
// past the 500-line default limit so a plain read is a truncated one.
const burstLines = 400

func startLogsLane(t *testing.T, env *harness.Env) *sut.Process {
	t.Helper()

	return sut.Start(t, sut.Config{
		Port:       logsPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE": "none",

			// Logs are reads: they must be served by a strictly read-only
			// deployment, not only by one that permits writes.
			"CETACEAN_OPERATIONS_LEVEL": "0",
		},
	})
}

// deployLogBurst deploys a service that writes a known, ordered burst to both
// streams and then idles, so a read has something to decode with content the
// test knows in advance.
func deployLogBurst(t *testing.T, env *harness.Env, proc *sut.Process) string {
	t.Helper()

	stack := fixtures.DeployStack(t, env, "logburst", []fixtures.ServiceSpec{{
		Name:     "app",
		Replicas: 1,
		Command: []string{fmt.Sprintf(
			`i=1; while [ $i -le %d ]; do `+
				`echo "ce2e-out-$i"; echo "ce2e-err-$i" >&2; i=$((i+1)); done; sleep infinity`,
			burstLines,
		)},
	}})

	id := serviceID(t, proc, stack+"_app")
	awaitCached(t, proc, "/services/"+id)

	return id
}

// deployLogTicker deploys a service that writes one line a second forever, for
// the cases that need output arriving while a stream is open.
func deployLogTicker(t *testing.T, env *harness.Env, proc *sut.Process) string {
	t.Helper()

	stack := fixtures.DeployStack(t, env, "logtick", []fixtures.ServiceSpec{{
		Name:     "app",
		Replicas: 1,
		Command:  []string{`i=1; while true; do echo "ce2e-tick-$i"; i=$((i+1)); sleep 1; done`},
	}})

	id := serviceID(t, proc, stack+"_app")
	awaitCached(t, proc, "/services/"+id)

	return id
}

// ─── reading ────────────────────────────────────────────────────────────

// logLine mirrors the fields internal/logs.LogLine serializes.
type logLine struct {
	Timestamp string            `json:"timestamp"`
	Message   string            `json:"message"`
	Stream    string            `json:"stream"`
	Attrs     map[string]string `json:"attrs"`
}

type logResponse struct {
	Lines   []logLine `json:"lines"`
	Oldest  string    `json:"oldest"`
	Newest  string    `json:"newest"`
	HasMore bool      `json:"hasMore"`
}

// readLogs issues a JSON log read and decodes it, failing on anything but 200.
func readLogs(t *testing.T, proc *sut.Process, path string) logResponse {
	t.Helper()

	out := precondRequest(t, proc, http.MethodGet, path, nil, "", "")
	if out.status != http.StatusOK {
		t.Fatalf("GET %s: status = %d (body: %s)", path, out.status, out.body)
	}

	// DetailResponse splices a struct's fields in beside the @-prefixed keys
	// rather than nesting them, so this decodes the envelope a client sees.
	var body logResponse
	if err := json.Unmarshal([]byte(out.body), &body); err != nil {
		t.Fatalf("decode %s: %v (body: %s)", path, err, out.body)
	}

	return body
}

// awaitLogLines polls a log read until it reports at least want lines, which a
// freshly started task needs: the container writes its burst on startup, but
// the read can land before the daemon has any of it.
func awaitLogLines(t *testing.T, proc *sut.Process, path string, want int) logResponse {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)

	for {
		body := readLogs(t, proc, path)
		if len(body.Lines) >= want {
			return body
		}

		if time.Now().After(deadline) {
			t.Fatalf("GET %s reported %d lines, want at least %d", path, len(body.Lines), want)
		}

		time.Sleep(time.Second)
	}
}

// TestServiceAndTaskLogsDecodeDockerFrames drives the multiplexed frame
// decoding on both routes: Docker sends an 8-byte header per frame naming the
// stream and payload size, and internal/logs turns that into timestamped
// lines. Nothing short of a real container produces those frames.
func TestServiceAndTaskLogsDecodeDockerFrames(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startLogsLane(t, env)
	service := deployLogBurst(t, env, proc)

	body := awaitLogLines(t, proc, "/services/"+service+"/logs?limit=10000", 2*burstLines)

	var stdout, stderr int

	for _, line := range body.Lines {
		switch line.Stream {
		case "stdout":
			stdout++
		case "stderr":
			stderr++
		default:
			t.Fatalf("line carries stream %q, which is neither stdout nor stderr", line.Stream)
		}

		if _, err := time.Parse(time.RFC3339Nano, line.Timestamp); err != nil {
			t.Fatalf("line %q carries an unparseable timestamp %q", line.Message, line.Timestamp)
		}

		// Details are requested from Docker (internal/docker/client.go's
		// Logs sets Details: true), and are what lets a client attribute an
		// interleaved service stream to the task that produced each line.
		if line.Attrs["taskId"] == "" {
			t.Fatalf("line %q carries no taskId attribute: %v", line.Message, line.Attrs)
		}
	}

	if stdout != burstLines || stderr != burstLines {
		t.Errorf(
			"decoded %d stdout and %d stderr lines, want %d of each",
			stdout, stderr, burstLines,
		)
	}

	// Docker interleaves tasks, so the handler sorts before truncating; a
	// client paginating by the reported cursors depends on that order.
	if !slices.IsSortedFunc(body.Lines, func(a, b logLine) int {
		return strings.Compare(a.Timestamp, b.Timestamp)
	}) {
		t.Error("lines are not ordered by timestamp")
	}

	if body.Oldest != body.Lines[0].Timestamp ||
		body.Newest != body.Lines[len(body.Lines)-1].Timestamp {
		t.Errorf(
			"oldest/newest = %q/%q, want the first and last line's timestamps %q/%q",
			body.Oldest, body.Newest,
			body.Lines[0].Timestamp, body.Lines[len(body.Lines)-1].Timestamp,
		)
	}

	// The task route reads the same container through a different Docker API.
	task := body.Lines[0].Attrs["taskId"]
	awaitCached(t, proc, "/tasks/"+task)

	fromTask := awaitLogLines(t, proc, "/tasks/"+task+"/logs?limit=10000", 2*burstLines)
	if len(fromTask.Lines) != len(body.Lines) {
		t.Errorf(
			"the task route decoded %d lines and the service route %d, for one task",
			len(fromTask.Lines), len(body.Lines),
		)
	}
}

// TestLogStreamFilterPartitionsTheOutput drives ?stream=: the two halves must
// partition the whole, which a filter that silently matched nothing — or
// everything — would fail.
func TestLogStreamFilterPartitionsTheOutput(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startLogsLane(t, env)
	service := deployLogBurst(t, env, proc)

	base := "/services/" + service + "/logs?limit=10000"
	all := awaitLogLines(t, proc, base, 2*burstLines)

	out := readLogs(t, proc, base+"&stream=stdout")
	errs := readLogs(t, proc, base+"&stream=stderr")

	for _, line := range out.Lines {
		if line.Stream != "stdout" {
			t.Fatalf("stream=stdout returned a %s line", line.Stream)
		}
	}

	for _, line := range errs.Lines {
		if line.Stream != "stderr" {
			t.Fatalf("stream=stderr returned a %s line", line.Stream)
		}
	}

	if len(out.Lines)+len(errs.Lines) != len(all.Lines) {
		t.Errorf(
			"stdout (%d) + stderr (%d) = %d, want the unfiltered %d",
			len(out.Lines), len(errs.Lines),
			len(out.Lines)+len(errs.Lines), len(all.Lines),
		)
	}
}

// TestLogCursorsAreEnforcedAfterParsing drives the rule internal/logs.FilterSince
// exists for: Docker ignores Since for service logs, so the cursor is applied to
// the parsed lines instead. Every documented form is driven, because each takes
// a different path through logs.ParseCursor. The expectation is computed from
// the unfiltered read by the same rule the contract states, so it holds whether
// the cursor lands inside the burst or outside it.
func TestLogCursorsAreEnforcedAfterParsing(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startLogsLane(t, env)
	service := deployLogBurst(t, env, proc)

	base := "/services/" + service + "/logs?limit=10000"
	all := awaitLogLines(t, proc, base, 2*burstLines)

	pivot := all.Lines[len(all.Lines)/2].Timestamp

	pivotTime, err := time.Parse(time.RFC3339Nano, pivot)
	if err != nil {
		t.Fatalf("parse pivot %q: %v", pivot, err)
	}

	for _, form := range []struct {
		name   string
		cursor string
	}{
		{"exact", pivot},
		{"second precision", pivotTime.UTC().Truncate(time.Second).Format(time.RFC3339)},
		{
			"non-UTC offset",
			pivotTime.In(time.FixedZone("plus-two", 2*3600)).Format(time.RFC3339Nano),
		},
	} {
		cutoff, err := time.Parse(time.RFC3339Nano, form.cursor)
		if err != nil {
			t.Fatalf("parse cursor %q: %v", form.cursor, err)
		}

		t.Run("after/"+form.name, func(t *testing.T) {
			assertLogLinesMatch(
				t,
				readLogs(t, proc, base+"&after="+url.QueryEscape(form.cursor)),
				linesBeyond(all.Lines, cutoff, true),
			)
		})

		t.Run("before/"+form.name, func(t *testing.T) {
			assertLogLinesMatch(
				t,
				readLogs(t, proc, base+"&before="+url.QueryEscape(form.cursor)),
				linesBeyond(all.Lines, cutoff, false),
			)
		})
	}

	// A duration means "that long ago", resolved against the server's clock
	// rather than compared as a string. Nothing here assumes how old the burst
	// is: 24h certainly predates it, 0s certainly does not.
	t.Run("after/duration", func(t *testing.T) {
		if old := readLogs(t, proc, base+"&after=24h"); len(old.Lines) != len(all.Lines) {
			t.Errorf(
				"after=24h returned %d of %d lines, though the cluster itself is younger",
				len(old.Lines), len(all.Lines),
			)
		}

		if now := readLogs(t, proc, base+"&after=0s"); len(now.Lines) != 0 {
			t.Errorf(
				"after=0s returned %d lines; no line can be newer than the moment the "+
					"request was served",
				len(now.Lines),
			)
		}
	})
}

// linesBeyond selects the lines strictly on one side of the cutoff: newer for a
// lower bound, older for an upper one. Both rules are strict, so a line stamped
// exactly at the cursor belongs to neither read; one whose timestamp cannot be
// placed belongs to both.
func linesBeyond(lines []logLine, cutoff time.Time, newer bool) []string {
	var want []string

	for _, line := range lines {
		at, err := time.Parse(time.RFC3339Nano, line.Timestamp)

		switch {
		case err != nil:
		case newer && !at.After(cutoff):
			continue
		case !newer && !at.Before(cutoff):
			continue
		}

		want = append(want, line.Timestamp+" "+line.Message)
	}

	return want
}

func assertLogLinesMatch(t *testing.T, got logResponse, want []string) {
	t.Helper()

	have := make([]string, 0, len(got.Lines))
	for _, line := range got.Lines {
		have = append(have, line.Timestamp+" "+line.Message)
	}

	if !slices.Equal(have, want) {
		t.Errorf(
			"the cursor selected %d lines, want the %d the same rule selects from the "+
				"unfiltered read",
			len(have), len(want),
		)
	}
}

// TestLogLimitBoundsTheResponse drives ?limit= and the hasMore flag it
// implies. hasMore can only be true once a cursor is in play: without one the
// handler asks Docker for exactly limit lines, so there is never a surplus to
// report.
func TestLogLimitBoundsTheResponse(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startLogsLane(t, env)
	service := deployLogBurst(t, env, proc)

	base := "/services/" + service + "/logs"
	all := awaitLogLines(t, proc, base+"?limit=10000", 2*burstLines)

	if capped := readLogs(t, proc, base+"?limit=7"); len(capped.Lines) != 7 {
		t.Errorf("limit=7 returned %d lines", len(capped.Lines))
	}

	// The default is 500 and the burst is larger, so an unparameterised read
	// is a truncated one.
	if def := readLogs(t, proc, base); len(def.Lines) != 500 {
		t.Errorf("the default read returned %d lines, want the documented 500", len(def.Lines))
	}

	// Above the documented maximum the request is served, not refused; it is
	// the response that is bounded.
	if over := readLogs(t, proc, base+"?limit=99999"); len(over.Lines) > 10000 {
		t.Errorf("limit=99999 returned %d lines, past the documented 10000", len(over.Lines))
	}

	// With a cursor the handler over-fetches, so a small limit leaves a
	// genuine surplus behind.
	oldest := all.Lines[0].Timestamp

	surplus := readLogs(t, proc, base+"?limit=5&after="+url.QueryEscape(oldest))
	if len(surplus.Lines) != 5 {
		t.Fatalf("limit=5 with a cursor returned %d lines", len(surplus.Lines))
	}

	if !surplus.HasMore {
		t.Error("hasMore is false though the cursor left far more than five lines behind")
	}

	// Truncation keeps the newest lines, which is what makes the reported
	// cursors usable for paging backwards.
	if surplus.Newest != all.Newest {
		t.Errorf(
			"the truncated read reports newest = %q, want the unfiltered %q",
			surplus.Newest, all.Newest,
		)
	}
}

// TestLogParametersAreValidatedServerSide drives the four refusals the handler
// owns. Each names its own code, so a client can tell which parameter it got
// wrong rather than being handed one undifferentiated 400.
func TestLogParametersAreValidatedServerSide(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startLogsLane(t, env)
	service := deployLogBurst(t, env, proc)

	base := "/services/" + service + "/logs"

	for _, tc := range []struct {
		name   string
		query  string
		accept string
		code   string
	}{
		{"stream", "?stream=stdlog", "", "LOG002"},
		{"after", "?after=not-a-time", "", "LOG003"},
		{"before", "?before=not-a-time", "", "LOG004"},

		// before is meaningless on a stream that only moves forwards, and is
		// refused rather than ignored.
		{"before on SSE", "?before=1h", "text/event-stream", "LOG005"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{}
			if tc.accept != "" {
				headers["Accept"] = tc.accept
			}

			out := precondRequest(t, proc, http.MethodGet, base+tc.query, headers, "", "")

			if out.status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", out.status, out.body)
			}

			if !strings.Contains(out.body, tc.code) {
				t.Errorf("the refusal does not name %s: %s", tc.code, out.body)
			}
		})
	}
}

// ─── streaming ──────────────────────────────────────────────────────────

// openLogStream opens an SSE log tail and returns its frame channel. The body
// is closed on cleanup, which is what ends the reader.
func openLogStream(
	t *testing.T,
	proc *sut.Process,
	path string,
	headers map[string]string,
) <-chan sseFrame {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proc.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", path, err)
	}

	req.Header.Set("Accept", "text/event-stream")

	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := proc.StreamClient().Do(req)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("open %s: status = %d", path, resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		resp.Body.Close()
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	frames := make(chan sseFrame, 64)
	done := make(chan struct{})

	t.Cleanup(func() {
		close(done)
		resp.Body.Close()
	})

	go readSSEFrames(resp.Body, frames, done)

	return frames
}

// awaitLogFrames collects want frames carrying a log line, or fails.
func awaitLogFrames(t *testing.T, frames <-chan sseFrame, want int) []sseFrame {
	t.Helper()

	var collected []sseFrame

	timeout := time.After(90 * time.Second)

	for len(collected) < want {
		select {
		case frame, ok := <-frames:
			if !ok {
				t.Fatalf("the stream ended after %d of %d frames", len(collected), want)
			}

			if frame.data == "" {
				continue // a keepalive comment carries no data
			}

			collected = append(collected, frame)
		case <-timeout:
			t.Fatalf("only %d of %d frames arrived within 90s", len(collected), want)
		}
	}

	return collected
}

func decodeLogFrame(t *testing.T, frame sseFrame) logLine {
	t.Helper()

	var line logLine
	if err := json.Unmarshal([]byte(frame.data), &line); err != nil {
		t.Fatalf("decode frame %q: %v", frame.data, err)
	}

	return line
}

// TestLogSSETailsLiveOutputAndResumes drives the follow stream: a fresh tail
// starts at now and carries an id per line, and a stream resumed from that id
// delivers only what came after. EventSource sends the id back as
// Last-Event-ID, which the handler reads as the cursor when no ?after= is given.
func TestLogSSETailsLiveOutputAndResumes(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startLogsLane(t, env)
	service := deployLogTicker(t, env, proc)

	path := "/services/" + service + "/logs"
	frames := awaitLogFrames(t, openLogStream(t, proc, path, nil), 3)

	var last string

	for _, frame := range frames {
		line := decodeLogFrame(t, frame)

		if !strings.HasPrefix(line.Message, "ce2e-tick-") {
			t.Fatalf("the tail delivered %q, which the ticker never wrote", line.Message)
		}

		if frame.id == "" {
			t.Fatalf("frame %q carries no id, so a client cannot resume from it", frame.data)
		}

		// Docker interleaves tasks, so a line can arrive stamped older than
		// one already sent; the id must never move backwards, or a resume
		// would skip the lines in between.
		if frame.id < last {
			t.Fatalf("the id moved backwards: %s then %s", last, frame.id)
		}

		last = frame.id
	}

	// Resume from the last id the way EventSource does.
	resumed := awaitLogFrames(
		t,
		openLogStream(t, proc, path, map[string]string{"Last-Event-ID": last}),
		1,
	)

	for _, frame := range resumed {
		line := decodeLogFrame(t, frame)

		if line.Timestamp <= last {
			t.Errorf(
				"the resumed stream replayed %s, which is not newer than the cursor %s",
				line.Timestamp, last,
			)
		}
	}
}

// TestLogSSEConnectionCapRefusesWithRetryAfter drives the published stream cap.
// The counter is incremented before the Docker call that precedes the response
// head, so a stream whose headers have arrived is already counted, and stays so
// until the handler returns. The last two are opened serially: the one that
// must still be admitted proves the cap is not lower than published, the one
// after it that it is not higher.
func TestLogSSEConnectionCapRefusesWithRetryAfter(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startLogsLane(t, env)
	service := deployLogTicker(t, env, proc)

	path := "/services/" + service + "/logs"

	open := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(
			t.Context(), http.MethodGet, proc.BaseURL+path, nil,
		)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Accept", "text/event-stream")

		return proc.StreamClient().Do(req)
	}

	// Each open costs a Docker attach, so holding them one at a time spends
	// minutes on a limit that is about concurrency, not order.
	var wg sync.WaitGroup

	held := make([]*http.Response, maxLogSSEConnsRef-1)
	errs := make([]error, maxLogSSEConnsRef-1)

	for i := range held {
		wg.Go(func() {
			held[i], errs[i] = open() //nolint:bodyclose // closed by the cleanup below
		})
	}

	wg.Wait()

	for i, resp := range held {
		if errs[i] != nil {
			t.Fatalf("open stream %d of %d: %v", i+1, maxLogSSEConnsRef, errs[i])
		}

		t.Cleanup(func() { resp.Body.Close() })

		if resp.StatusCode != http.StatusOK {
			t.Fatalf(
				"stream %d of %d was refused with %d; the handler ran out of room "+
					"before the published cap",
				i+1, maxLogSSEConnsRef, resp.StatusCode,
			)
		}
	}

	last, err := open()
	if err != nil {
		t.Fatalf("open stream %d: %v", maxLogSSEConnsRef, err)
	}

	t.Cleanup(func() { last.Body.Close() })

	if last.StatusCode != http.StatusOK {
		t.Fatalf(
			"stream %d was refused with %d, one short of the published cap",
			maxLogSSEConnsRef, last.StatusCode,
		)
	}

	// Opened like the others rather than through send: a stream the cap wrongly
	// admitted never ends, and reading its body to completion would report the
	// client's own timeout instead of the surplus that caused it.
	refused, err := open()
	if err != nil {
		t.Fatalf("open stream %d: %v", maxLogSSEConnsRef+1, err)
	}

	defer refused.Body.Close()

	if refused.StatusCode != http.StatusTooManyRequests {
		t.Fatalf(
			"stream %d: status = %d, want 429; the handler admitted more than the "+
				"published cap of %d",
			maxLogSSEConnsRef+1, refused.StatusCode, maxLogSSEConnsRef,
		)
	}

	if got := refused.Header.Get("Retry-After"); got != "5" {
		t.Errorf("Retry-After = %q, want 5", got)
	}

	// RFC 9457, like every other error this API produces. A refusal in some
	// other shape is one a client's existing error handling cannot read.
	if ct := refused.Header.Get("Content-Type"); !strings.HasPrefix(
		ct, "application/problem+json",
	) {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}

	body, err := io.ReadAll(refused.Body)
	if err != nil {
		t.Fatalf("read the refusal body: %v", err)
	}

	if !strings.Contains(string(body), "LOG001") {
		t.Errorf("the refusal does not name LOG001: %s", body)
	}
}

// ─── the gate ───────────────────────────────────────────────────────────

// drivenLogRoutes is the inventory this lane covers.
var drivenLogRoutes = map[string]bool{
	"GET /services/{id}/logs": true,
	"GET /tasks/{id}/logs":    true,
}

// TestEveryLogRouteIsDriven fails when the router gains a log endpoint this
// lane does not drive.
func TestEveryLogRouteIsDriven(t *testing.T) {
	live := map[string]bool{}

	for _, route := range contractRoutes(t) {
		if !strings.HasSuffix(route.Pattern, "/logs") {
			continue
		}

		live[route.String()] = true

		if !drivenLogRoutes[route.String()] {
			t.Errorf("%s is a log endpoint this lane does not drive", route)
		}
	}

	for key := range drivenLogRoutes {
		if !live[key] {
			t.Errorf("drivenLogRoutes has a stale entry %q: no such log route", key)
		}
	}

	if len(live) == 0 {
		t.Fatal("the route inventory reports no log endpoints at all")
	}
}
