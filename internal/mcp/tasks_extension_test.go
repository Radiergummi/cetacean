package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/spec"
)

// The Tasks extension as this server answers it on the 2026-07-28 wire. The
// deferred requirements are pinned to today's answer, so a fix fails here and
// the registry entry has to move with it.

// rawResult issues one modern request and decodes its result as a JSON object.
func rawResult(t *testing.T, handler http.Handler, method, params string) map[string]any {
	t.Helper()

	_, envelope := mcpModern(t, handler, 3, method, params)
	if envelope.Error != nil {
		t.Fatalf("%s: %+v", method, envelope.Error)
	}

	var out map[string]any
	if err := json.Unmarshal(envelope.Result, &out); err != nil {
		t.Fatalf("%s: decode result: %v (raw %s)", method, err, envelope.Result)
	}

	return out
}

func taskParamsJSON(taskID string) string { return fmt.Sprintf(`{"taskId":%q}`, taskID) }

func TestTaskCreationOnTheModernWire(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "web", 3, 1)
	handler := taskTestServer(t, c).Handler()

	call := `{"name":"scale_service","arguments":{"id":"web","replicas":3},"task":{}}`

	t.Run("the task resolves as soon as the handle is returned", func(t *testing.T) {
		spec.Satisfies(t, "mcp/sep-2663/durable-create-strong-consistency")

		created := rawResult(t, handler, "tools/call", call)
		task, _ := created["task"].(map[string]any)
		id, _ := task["taskId"].(string)

		_, envelope := mcpModern(t, handler, 4, "tasks/get", taskParamsJSON(id))
		if envelope.Error != nil {
			t.Fatalf("tasks/get straight after creation: %+v", envelope.Error)
		}
	})

	// withProtocolMeta declares no extension, so each of these is a client
	// the extension forbids handing a task to.
	t.Run("a client that declares no extension still gets a task", func(t *testing.T) {
		spec.Satisfies(t,
			"mcp/sep-2663/server-rejects-undeclared-client",
			"mcp/sep-2133/unsupported-extension-falls-back",
		)

		created := rawResult(t, handler, "tools/call", call)
		if _, ok := created["task"].(map[string]any); !ok {
			t.Fatalf("want today's CreateTaskResult for a legacy opt-in, got %v", created)
		}
	})

	t.Run("the create-task result is typed complete", func(t *testing.T) {
		spec.Satisfies(t, "mcp/sep-2663/result-type-task-on-create")

		created := rawResult(t, handler, "tools/call", call)
		if got := created["resultType"]; got != string(mcplib.ResultTypeComplete) {
			t.Errorf("resultType = %v, want today's %q", got, mcplib.ResultTypeComplete)
		}
	})

	t.Run("tasks/get serves a client that declares no extension", func(t *testing.T) {
		spec.Satisfies(t, "mcp/sep-2663/tasks-methods-non-declaring")

		handle := callAsTask(t, handler, "scale_service",
			map[string]any{"id": "web", "replicas": float64(3)})

		if got := pollTask(t, handler, handle.Task.TaskId); got.Task.TaskId == "" {
			t.Error("tasks/get answered no task")
		}
	})
}

// Nothing but a CreateTaskResult may say "task"; the extension's own methods
// answer the standard shape.
func TestOnlyACreateTaskResultIsTypedTask(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "web", 3, 1)
	handler := taskTestServer(t, c).Handler()

	handle := callAsTask(t, handler, "scale_service",
		map[string]any{"id": "web", "replicas": float64(3)})

	complete := func(t *testing.T, method, params string) {
		t.Helper()

		result := rawResult(t, handler, method, params)
		if got := result["resultType"]; got != string(mcplib.ResultTypeComplete) {
			t.Errorf("%s: resultType = %v, want %q", method, got, mcplib.ResultTypeComplete)
		}
	}

	t.Run("the core methods", func(t *testing.T) {
		spec.Satisfies(t, "mcp/sep-2663/no-task-result-type-on-others")

		complete(t, "server/discover", `{}`)
		complete(t, "tools/list", `{}`)
		complete(t, "tools/call", `{"name":"find","arguments":{"query":"web"}}`)
	})

	t.Run("tasks/get", func(t *testing.T) {
		spec.Satisfies(t,
			"mcp/sep-2663/no-task-result-type-on-others",
			"mcp/sep-2663/result-type-complete-on-tasks-get",
		)

		complete(t, "tasks/get", taskParamsJSON(handle.Task.TaskId))
	})

	t.Run("tasks/cancel", func(t *testing.T) {
		spec.Satisfies(t,
			"mcp/sep-2663/no-task-result-type-on-others",
			"mcp/sep-2663/result-type-complete-on-tasks-cancel",
		)

		complete(t, "tasks/cancel", taskParamsJSON(handle.Task.TaskId))
	})
}

func TestTaskTerminalStatesOnTheModernWire(t *testing.T) {
	t.Run("a completed task carries no result", func(t *testing.T) {
		spec.Satisfies(t, "mcp/sep-2663/tasks-get-status-completed")

		c := cache.New(nil)
		seedService(t, c, "web", 3, 1)
		handler := taskTestServer(t, c).Handler()

		handle := callAsTask(t, handler, "scale_service",
			map[string]any{"id": "web", "replicas": float64(3)})
		seedService(t, c, "web", 3, 3)
		awaitTerminal(t, handler, handle.Task.TaskId, 5*time.Second)

		got := rawResult(t, handler, "tasks/get", taskParamsJSON(handle.Task.TaskId))
		if got["status"] != string(mcplib.TaskStatusCompleted) {
			t.Fatalf("status = %v, want completed", got["status"])
		}

		if _, ok := got["result"]; ok {
			t.Errorf("tasks/get now carries the result; move the requirement out of deferred")
		}
	})

	t.Run("a cancelled task reads cancelled, and the cancel answers the task", func(t *testing.T) {
		spec.Satisfies(t,
			"mcp/sep-2663/tasks-get-status-cancelled",
			"mcp/sep-2663/cancel-ack-empty-result",
		)

		c := cache.New(nil)
		seedService(t, c, "web", 3, 1)
		handler := taskTestServer(t, c).Handler()

		handle := callAsTask(t, handler, "scale_service",
			map[string]any{"id": "web", "replicas": float64(3)})

		ack := rawResult(t, handler, "tasks/cancel", taskParamsJSON(handle.Task.TaskId))
		if ack["taskId"] != handle.Task.TaskId {
			t.Errorf("tasks/cancel answered %v, want today's cancelled Task", ack)
		}

		if got := pollTask(t, handler, handle.Task.TaskId); got.Task.Status !=
			string(mcplib.TaskStatusCancelled) {
			t.Errorf("status = %q, want cancelled", got.Task.Status)
		}
	})

	t.Run("a failed task explains itself in statusMessage alone", func(t *testing.T) {
		spec.Satisfies(t,
			"mcp/sep-2663/tasks-get-status-failed",
			"mcp/sep-2663/failed-includes-statusmessage",
		)

		c := cache.New(nil)
		seedService(t, c, "web", 2, 2)
		handler := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsOperational).Handler()

		handle := callAsTask(t, handler, "scale_service",
			map[string]any{"id": "web", "replicas": float64(3)})
		awaitTerminal(t, handler, handle.Task.TaskId, 5*time.Second)

		got := rawResult(t, handler, "tasks/get", taskParamsJSON(handle.Task.TaskId))
		msg, _ := got["statusMessage"].(string)
		if got["status"] != string(mcplib.TaskStatusFailed) || msg == "" {
			t.Fatalf("want a failed task with a statusMessage, got %v", got)
		}

		if _, ok := got["error"]; ok {
			t.Errorf("a failed task now carries error; move the requirement out of deferred")
		}
	})
}

func TestTaskMethodsRefuseAnUnknownTask(t *testing.T) {
	handler := taskTestServer(t, cache.New(nil)).Handler()

	t.Run("tasks/get", func(t *testing.T) {
		spec.Satisfies(t, "mcp/sep-2663/tasks-get-invalid-task-id-32602")

		assertTaskError(t, handler, "tasks/get", mcplib.INVALID_PARAMS)
	})

	t.Run("tasks/cancel", func(t *testing.T) {
		spec.Satisfies(t, "mcp/sep-2663/cancel-error-unknown-task-id")

		assertTaskError(t, handler, "tasks/cancel", mcplib.INVALID_PARAMS)
	})

	// Pinned whole: the method does not exist, which answers every one of
	// these the same way.
	t.Run("tasks/update is not implemented", func(t *testing.T) {
		spec.Satisfies(t,
			"mcp/sep-2663/update-ack-empty-result",
			"mcp/sep-2663/update-error-unknown-task-id",
			"mcp/sep-2663/update-ignore-unknown-keys",
			"mcp/sep-2663/result-type-complete-on-tasks-update",
			"mcp/sep-2663/tasks-update-cancel-invalid-task-id-32602",
		)

		assertTaskError(t, handler, "tasks/update", mcplib.METHOD_NOT_FOUND)
	})
}

func assertTaskError(t *testing.T, handler http.Handler, method string, code int) {
	t.Helper()

	_, envelope := mcpModern(t, handler, 5, method,
		`{"taskId":"no-such-task","inputResponses":{}}`)
	if envelope.Error == nil || envelope.Error.Code != code {
		t.Errorf("%s on an unknown task: error %+v, want code %d", method, envelope.Error, code)
	}
}

// A listen for task status from a client that declares no Tasks extension is
// acknowledged with the task IDs dropped, rather than refused.
func TestListenForTaskStatusWithoutTheExtension(t *testing.T) {
	spec.Satisfies(t, "mcp/sep-2663/subscribe-missing-capability-non-declaring")

	server := httptest.NewServer(taskTestServer(t, cache.New(nil)).Handler())
	t.Cleanup(server.Close)

	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"subscriptions/listen","params":%s}`,
		withProtocolMeta(t, `{"notifications":{"taskIds":["some-task"]}}`))

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/mcp",
		strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(mcplib.HeaderProtocolVersion, mcplib.LATEST_PROTOCOL_VERSION)
	req.Header.Set(mcplib.HeaderMethod, "subscriptions/listen")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	// Nothing is left to listen for once the task IDs are dropped, so the
	// stream ends on its own; whether the acknowledgement makes it out before
	// the result is mcp-go's race, not part of the answer.
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("the listen stream did not end on its own: %v (read %s)", err, raw)
	}

	frames := 0

	for line := range strings.Lines(string(raw)) {
		payload, ok := strings.CutPrefix(strings.TrimSpace(line), "data: ")
		if !ok {
			continue
		}

		frames++

		var frame struct {
			Error  any `json:"error"`
			Params struct {
				Notifications map[string]any `json:"notifications"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			t.Fatalf("frame is not JSON-RPC: %v (%s)", err, payload)
		}

		if frame.Error != nil {
			t.Errorf("the listen is now refused; move the requirement out of deferred: %s", payload)
		}

		if _, agreed := frame.Params.Notifications["taskIds"]; agreed {
			t.Errorf("the acknowledgement agrees to taskIds: %s", payload)
		}
	}

	if frames == 0 {
		t.Fatalf("the listen stream carried no frame: %s", raw)
	}
}
