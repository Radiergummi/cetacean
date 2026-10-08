package mcp

import (
	"encoding/json"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// Characterizes SEP-1303 conformance: a tool's own input-validation failure must
// surface as a Tool Execution Error — a tools/call result with isError=true —
// and not as a JSON-RPC protocol error. The invariant per call is no protocol
// error and an isError result.
func TestToolInputValidationIsExecutionError(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsOperational)
	handler := srv.Handler()

	calls := []struct {
		name string
		args string
	}{
		// scale_service rejects replicas < 0.
		{"scale_service", `{"id":"svc1","replicas":-1}`},
		// find rejects an empty query when type is omitted.
		{"find", `{"query":""}`},
	}

	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			_, env := mcpModern(t, handler, 2, "tools/call",
				`{"name":"`+call.name+`","arguments":`+call.args+`}`)

			// Teeth: a regression to a protocol-level error would set
			// env.Error here, failing the assertion.
			if env.Error != nil {
				t.Fatalf(
					"%s: validation failure surfaced as a JSON-RPC protocol error: %+v",
					call.name, env.Error,
				)
			}

			var result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(env.Result, &result); err != nil {
				t.Fatalf("%s: decode result: %v: %s", call.name, err, string(env.Result))
			}

			if !result.IsError {
				t.Fatalf(
					"%s: expected a Tool Execution Error result (isError=true), got %s",
					call.name, string(env.Result),
				)
			}
			if len(result.Content) == 0 || result.Content[0].Text == "" {
				t.Errorf(
					"%s: expected non-empty error text describing the problem, got %s",
					call.name, string(env.Result),
				)
			}
		})
	}
}
