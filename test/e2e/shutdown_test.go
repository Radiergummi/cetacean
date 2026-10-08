//go:build e2e

package e2e_test

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives what happens to a request in flight when the process is
// asked to stop — the moment an orchestrator puts the binary through on every
// rolling update. It reserves port 19021 (see README.md's reserved-ports
// table).

const shutdownPort = 19021

// slowQuery is the PromQL this lane drives. The delay is keyed on it because
// the SUT queries the same upstream on its own account, and a blanket delay
// would hand the case someone else's request to synchronise on.
const slowQuery = "up"

// slowUpstream stands in for Prometheus, holding slowQuery open for a known
// interval so the SUT has a handler still running when the signal arrives.
type slowUpstream struct {
	server   *http.Server
	url      string
	received chan struct{}
	announce sync.Once
}

func startSlowUpstream(t *testing.T, delay time.Duration) *slowUpstream {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	up := &slowUpstream{
		url:      "http://" + listener.Addr().String(),
		received: make(chan struct{}),
	}

	const emptyVector = `{"status":"success","data":{"resultType":"vector","result":[]}}`

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == slowQuery {
			up.announce.Do(func() { close(up.received) })

			time.Sleep(delay)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, emptyVector)
	})

	up.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() { _ = up.server.Serve(listener) }()

	t.Cleanup(func() { _ = up.server.Close() })

	return up
}

// TestInFlightRequestSurvivesGracefulShutdown asserts that a request already
// being served when SIGINT arrives is answered rather than cut off.
func TestInFlightRequestSurvivesGracefulShutdown(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	// Inside the product's five-second shutdown grace, so only a server that
	// does not wait at all fails this.
	upstream := startSlowUpstream(t, 1500*time.Millisecond)

	proc := sut.Start(t, sut.Config{
		Port:       shutdownPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":      "none",
			"CETACEAN_PROMETHEUS_URL": upstream.url,
		},
	})

	type outcome struct {
		status int
		err    error
	}

	done := make(chan outcome, 1)

	go func() {
		request, err := http.NewRequest(
			http.MethodGet,
			proc.BaseURL+"/metrics?query="+slowQuery,
			nil,
		)
		if err != nil {
			done <- outcome{err: err}

			return
		}

		request.Header.Set("Accept", "application/json")

		response, err := proc.Client().Do(request)
		if err != nil {
			done <- outcome{err: err}

			return
		}

		defer response.Body.Close()

		// A truncated response can carry a status line and still fail here.
		if _, err := io.ReadAll(response.Body); err != nil {
			done <- outcome{status: response.StatusCode, err: err}

			return
		}

		done <- outcome{status: response.StatusCode}
	}()

	// Signal only once the handler is provably inside the upstream call.
	select {
	case <-upstream.received:
	case <-time.After(15 * time.Second):
		t.Fatal("the proxied request never reached the upstream")
	}

	proc.Stop()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf(
				"in-flight request did not survive shutdown: %v\n"+
					"the process exited while this request was still being served, "+
					"so a rolling update truncates whatever the dashboard was doing",
				got.err,
			)
		}

		if got.status != http.StatusOK {
			t.Errorf("in-flight request answered %d, want 200", got.status)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("in-flight request neither completed nor failed")
	}
}

// TestShutdownStopsAcceptingNewConnections is the other half: draining must
// not keep the door open for new work.
func TestShutdownStopsAcceptingNewConnections(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	upstream := startSlowUpstream(t, 1500*time.Millisecond)

	proc := sut.Start(t, sut.Config{
		Port:       shutdownPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":      "none",
			"CETACEAN_PROMETHEUS_URL": upstream.url,
		},
	})

	started := make(chan struct{})

	go func() {
		defer close(started)

		request, _ := http.NewRequest(http.MethodGet, proc.BaseURL+"/metrics?query="+slowQuery, nil)
		request.Header.Set("Accept", "application/json")

		response, err := proc.Client().Do(request)
		if err == nil {
			_, _ = io.ReadAll(response.Body)
			_ = response.Body.Close()
		}
	}()

	select {
	case <-upstream.received:
	case <-time.After(15 * time.Second):
		t.Fatal("the proxied request never reached the upstream")
	}

	proc.Stop()
	<-started

	// A fresh connection after the process is gone must be refused outright.
	response, err := http.Get(proc.BaseURL + "/-/health") //nolint:noctx // post-shutdown probe
	if err == nil {
		_ = response.Body.Close()

		t.Fatal("the listener still accepted a connection after shutdown")
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Errorf("connection after shutdown timed out rather than being refused: %v", err)
	}
}
