//go:build e2e

package e2e_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives CETACEAN_OTEL_ENDPOINT: the unit tests cover the adapters,
// nothing else covers whether they are built and handed to mcp-go at all. It
// reserves port 19023 (see README.md's reserved-ports table).

const tracingPort = 19023

// spanCollector stands in for an OTLP/HTTP collector. It does not decode the
// protobuf: a POST to /v1/traces carrying a body is the whole claim.
type spanCollector struct {
	server *httptest.Server

	mu       sync.Mutex
	exports  int
	lastSize int
}

func startSpanCollector(t *testing.T) *spanCollector {
	t.Helper()

	collector := &spanCollector{}

	collector.server = httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)

			if r.URL.Path == "/v1/traces" && len(body) > 0 {
				collector.mu.Lock()
				collector.exports++
				collector.lastSize = len(body)
				collector.mu.Unlock()
			}

			// An empty 200 is a valid ExportTraceServiceResponse; the exporter
			// retries anything else, which would hang rather than fail.
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.WriteHeader(http.StatusOK)
		}))

	t.Cleanup(collector.server.Close)

	return collector
}

func (c *spanCollector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.exports
}

// TestMCPToolCallIsExportedAsASpan drives the whole pipeline, from the
// configured endpoint to a span reaching the collector.
func TestMCPToolCallIsExportedAsASpan(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	collector := startSpanCollector(t)

	proc := sut.Start(t, sut.Config{
		Port:       tracingPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":     "none",
			"CETACEAN_MCP":           "true",
			"CETACEAN_OTEL_ENDPOINT": collector.server.URL,
		},
	})

	if got := collector.count(); got != 0 {
		t.Fatalf("collector saw %d exports before anything was driven", got)
	}

	mcpCall(t, proc, "tools/list", nil)
	mcpCall(t, proc, "tools/call", map[string]any{
		"name":      "find",
		"arguments": map[string]any{"type": "services"},
	})

	// The provider batches, so shutting the SUT down is what flushes — and is
	// itself the path that matters.
	proc.Stop()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if collector.count() > 0 {
			break
		}

		time.Sleep(250 * time.Millisecond)
	}

	if collector.count() == 0 {
		t.Fatalf(
			"no spans reached the collector at %s\n"+
				"CETACEAN_OTEL_ENDPOINT is set, MCP is enabled and two tool calls were "+
				"driven, so either the provider was not built, not handed to mcp-go, or "+
				"not flushed on shutdown\nSUT logs:\n%s",
			collector.server.URL, proc.Logs(),
		)
	}
}

// TestTracingWithoutMCPStartsAndWarns pins the combination that does nothing:
// MCP is the only thing emitting spans. It must still start.
func TestTracingWithoutMCPStartsAndWarns(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	collector := startSpanCollector(t)

	proc := sut.Start(t, sut.Config{
		Port:       tracingPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":     "none",
			"CETACEAN_MCP":           "false",
			"CETACEAN_OTEL_ENDPOINT": collector.server.URL,
		},
	})

	response, err := proc.Client().Get(proc.BaseURL + "/-/ready") //nolint:noctx // test probe
	if err != nil {
		t.Fatalf("GET /-/ready: %v", err)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Errorf("GET /-/ready = %d with tracing configured but MCP off", response.StatusCode)
	}

	if !strings.Contains(proc.Logs(), "no spans will be exported") {
		t.Errorf(
			"tracing configured without MCP did not warn that nothing will be exported\n"+
				"logs:\n%s",
			proc.Logs(),
		)
	}
}

// TestMalformedTracingEndpointFailsFast pins why NewProvider validates the
// endpoint itself: the exporter would fall back to localhost:4318, so bad
// configuration would look accepted.
func TestMalformedTracingEndpointFailsFast(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	code, logs := sut.StartExpectingExit(t, sut.Config{
		Port:       tracingPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":     "none",
			"CETACEAN_MCP":           "true",
			"CETACEAN_OTEL_ENDPOINT": "://not-a-url",
		},
	})

	if code == 0 {
		t.Errorf("the binary exited 0 on an endpoint it cannot export to\n%s", logs)
	}

	if !strings.Contains(logs, "tracing") {
		t.Errorf(
			"the refusal does not name tracing, so an operator cannot tell which "+
				"setting is wrong\n%s",
			logs,
		)
	}
}
