//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives what Cetacean does when the Docker API goes away underneath
// it — the one failure a read-through cache hides best, since every endpoint
// keeps answering from state that has stopped moving. It reserves port 19022
// (see README.md's reserved-ports table).

const connectivityPort = 19022

// dockerBreaker is a TCP passthrough to the engine that a case can sever and
// restore, without restarting the container every other lane shares.
type dockerBreaker struct {
	address string

	mu       sync.Mutex
	listener net.Listener
	conns    []net.Conn
	upstream string
	closed   bool
}

func startDockerBreaker(t *testing.T, dockerHost string) *dockerBreaker {
	t.Helper()

	upstream := strings.TrimPrefix(dockerHost, "tcp://")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	breaker := &dockerBreaker{
		address:  "tcp://" + listener.Addr().String(),
		listener: listener,
		upstream: upstream,
	}

	go breaker.serve(listener)

	t.Cleanup(breaker.Break)

	return breaker
}

func (b *dockerBreaker) serve(listener net.Listener) {
	for {
		client, err := listener.Accept()
		if err != nil {
			return
		}

		server, err := net.Dial("tcp", b.upstream)
		if err != nil {
			_ = client.Close()

			continue
		}

		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			_ = client.Close()
			_ = server.Close()

			return
		}

		b.conns = append(b.conns, client, server)
		b.mu.Unlock()

		go func() { _, _ = io.Copy(server, client) }()
		go func() { _, _ = io.Copy(client, server) }()
	}
}

// Break severs the connection the way a stopped daemon does: new dials are
// refused, and established connections drop so the event stream fails rather
// than hangs.
func (b *dockerBreaker) Break() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.closed = true

	if b.listener != nil {
		_ = b.listener.Close()
		b.listener = nil
	}

	for _, conn := range b.conns {
		_ = conn.Close()
	}

	b.conns = nil
}

// Restore brings the passthrough back on the same address.
func (b *dockerBreaker) Restore(t *testing.T) {
	t.Helper()

	b.mu.Lock()
	defer b.mu.Unlock()

	listener, err := net.Listen("tcp", strings.TrimPrefix(b.address, "tcp://"))
	if err != nil {
		t.Fatalf("re-listen on %s: %v", b.address, err)
	}

	b.closed = false
	b.listener = listener

	go b.serve(listener)
}

// watcherHealth is the cache-freshness half of GET /-/health.
type watcherHealth struct {
	Connected          bool     `json:"connected"`
	LastSyncAgeSeconds *float64 `json:"lastSyncAgeSeconds"`
}

type healthBody struct {
	Status  string         `json:"status"`
	Watcher *watcherHealth `json:"watcher"`
}

func readHealth(t *testing.T, proc *sut.Process) healthBody {
	t.Helper()

	response, err := proc.Client().Get(proc.BaseURL + "/-/health") //nolint:noctx // test probe
	if err != nil {
		t.Fatalf("GET /-/health: %v", err)
	}

	defer response.Body.Close()

	var body healthBody
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("GET /-/health: decode: %v", err)
	}

	if body.Watcher == nil {
		t.Fatal("GET /-/health carries no watcher block; nothing reports cache freshness")
	}

	return body
}

// awaitConnected polls /-/health until the watcher reports the wanted state.
func awaitConnected(t *testing.T, proc *sut.Process, want bool, within time.Duration) healthBody {
	t.Helper()

	deadline := time.Now().Add(within)

	var last healthBody

	for time.Now().Before(deadline) {
		last = readHealth(t, proc)

		if last.Watcher.Connected == want {
			return last
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Fatalf(
		"watcher still reports connected=%v after %s, want %v",
		last.Watcher.Connected, within, want,
	)

	return last
}

// TestHealthReportsALostDockerConnection is the case an observability tool
// cannot afford to fail: with the engine gone, every endpoint keeps answering
// and readiness keeps passing, so nothing else says the state is stale.
func TestHealthReportsALostDockerConnection(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	breaker := startDockerBreaker(t, env.DockerHost)

	proc := sut.Start(t, sut.Config{
		Port:       connectivityPort,
		DockerHost: breaker.address,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE": "none",
		},
	})

	t.Run("a healthy watcher reports connected with a fresh sync", func(t *testing.T) {
		body := awaitConnected(t, proc, true, 30*time.Second)

		if body.Watcher.LastSyncAgeSeconds == nil {
			t.Fatal("no lastSyncAgeSeconds after a successful sync")
		}

		if *body.Watcher.LastSyncAgeSeconds > 60 {
			t.Errorf(
				"lastSyncAgeSeconds = %v straight after a sync",
				*body.Watcher.LastSyncAgeSeconds,
			)
		}
	})

	servicesBefore := countServices(t, proc)

	breaker.Break()

	t.Run("a severed connection is reported", func(t *testing.T) {
		awaitConnected(t, proc, false, 60*time.Second)
	})

	t.Run("the cache is still served, and readiness still passes", func(t *testing.T) {
		// Deliberate: the container healthcheck polls /-/ready, and restarting
		// over an engine outage fixes nothing.
		response, err := proc.Client().Get(proc.BaseURL + "/-/ready") //nolint:noctx // test probe
		if err != nil {
			t.Fatalf("GET /-/ready: %v", err)
		}

		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			t.Errorf("GET /-/ready = %d while the engine is down, want 200", response.StatusCode)
		}

		if got := countServices(t, proc); got != servicesBefore {
			t.Errorf("services listing returned %d with the engine down, want the cached %d",
				got, servicesBefore)
		}
	})

	t.Run("the connection is picked back up", func(t *testing.T) {
		breaker.Restore(t)

		// The watcher's backoff doubles to 30s; allow for a full one.
		awaitConnected(t, proc, true, 90*time.Second)
	})
}

// TestWatcherMetricsReportTheConnection pins the same facts on /-/metrics.
func TestWatcherMetricsReportTheConnection(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	breaker := startDockerBreaker(t, env.DockerHost)

	proc := sut.Start(t, sut.Config{
		Port:       connectivityPort,
		DockerHost: breaker.address,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":    "none",
			"CETACEAN_SELF_METRICS": "true",
		},
	})

	awaitConnected(t, proc, true, 30*time.Second)

	metrics := scrapeMetrics(t, proc)

	if got := metrics["cetacean_watcher_connected"]; got != 1 {
		t.Errorf("cetacean_watcher_connected = %v while connected, want 1", got)
	}

	if metrics["cetacean_cache_last_sync_timestamp_seconds"] == 0 {
		t.Error("cetacean_cache_last_sync_timestamp_seconds is unset after a successful sync")
	}

	breaker.Break()
	awaitConnected(t, proc, false, 60*time.Second)

	metrics = scrapeMetrics(t, proc)

	if got := metrics["cetacean_watcher_connected"]; got != 0 {
		t.Errorf("cetacean_watcher_connected = %v with the engine down, want 0", got)
	}

	// The failure counter only moves on the next sync attempt, after the
	// backoff.
	awaitMetricAbove(t, proc, "cetacean_cache_sync_failures_total", 0, 30*time.Second)
}

// awaitMetricAbove polls /-/metrics until a sample exceeds want.
func awaitMetricAbove(
	t *testing.T,
	proc *sut.Process,
	name string,
	want float64,
	within time.Duration,
) {
	t.Helper()

	deadline := time.Now().Add(within)

	var last float64

	for time.Now().Before(deadline) {
		last = scrapeMetrics(t, proc)[name]

		if last > want {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Errorf("%s = %v after %s, want more than %v", name, last, within, want)
}

// scrapeMetrics reads /-/metrics into a map of the unlabelled samples this
// lane asserts on.
func scrapeMetrics(t *testing.T, proc *sut.Process) map[string]float64 {
	t.Helper()

	response, err := proc.Client().Get(proc.BaseURL + "/-/metrics") //nolint:noctx // test probe
	if err != nil {
		t.Fatalf("GET /-/metrics: %v", err)
	}

	defer response.Body.Close()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("GET /-/metrics: read: %v", err)
	}

	out := map[string]float64{}

	for line := range strings.SplitSeq(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name, value, found := strings.Cut(line, " ")
		if !found || strings.Contains(name, "{") {
			continue
		}

		var parsed float64
		if _, err := fmt.Sscanf(value, "%g", &parsed); err != nil {
			continue
		}

		out[name] = parsed
	}

	return out
}

// countServices reads the services listing's total.
func countServices(t *testing.T, proc *sut.Process) int {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, proc.BaseURL+"/services", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	request.Header.Set("Accept", "application/json")

	response, err := proc.Client().Do(request)
	if err != nil {
		t.Fatalf("GET /services: %v", err)
	}

	defer response.Body.Close()

	var body struct {
		Total int `json:"total"`
	}

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("GET /services: decode: %v", err)
	}

	return body.Total
}
