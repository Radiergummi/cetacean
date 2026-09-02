package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// taskHandle is the create-task response a task-augmented tools/call returns
// instead of the tool's own result.
type taskHandle struct {
	Task struct {
		TaskId        string `json:"taskId"`
		Status        string `json:"status"`
		StatusMessage string `json:"statusMessage"`
	} `json:"task"`
}

// callAsTask issues a tools/call carrying the task augmentation, the way a
// client asks for a mutation it intends to poll rather than wait on.
func callAsTask(
	t *testing.T,
	handler http.Handler,
	name string,
	arguments map[string]any,
) taskHandle {
	t.Helper()

	params, err := json.Marshal(map[string]any{
		"name":      name,
		"arguments": arguments,
		"task":      map[string]any{},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, envelope := mcpModern(t, handler, 1, "tools/call", string(params))
	if envelope.Error != nil {
		t.Fatalf("tools/call as task failed: %+v", envelope.Error)
	}

	var handle taskHandle
	if err := json.Unmarshal(envelope.Result, &handle); err != nil {
		t.Fatalf("decode create-task result: %v (raw %s)", err, envelope.Result)
	}

	if handle.Task.TaskId == "" {
		t.Fatalf("no task handle returned; got %s", envelope.Result)
	}

	return handle
}

// pollTask reads tasks/get once.
func pollTask(t *testing.T, handler http.Handler, taskID string) taskHandle {
	t.Helper()

	_, envelope := mcpModern(t, handler, 2, "tasks/get", fmt.Sprintf(`{"taskId":%q}`, taskID))
	if envelope.Error != nil {
		t.Fatalf("tasks/get failed: %+v", envelope.Error)
	}

	// tasks/get inlines the Task fields on the result rather than nesting them.
	var status struct {
		TaskId        string `json:"taskId"`
		Status        string `json:"status"`
		StatusMessage string `json:"statusMessage"`
	}

	if err := json.Unmarshal(envelope.Result, &status); err != nil {
		t.Fatalf("decode tasks/get: %v (raw %s)", err, envelope.Result)
	}

	return taskHandle{Task: status}
}

// awaitTerminal polls until the task leaves "working".
func awaitTerminal(
	t *testing.T,
	handler http.Handler,
	taskID string,
	within time.Duration,
) taskHandle {
	t.Helper()

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		got := pollTask(t, handler, taskID)
		if got.Task.Status != string(mcplib.TaskStatusWorking) {
			return got
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("task %s never left %q", taskID, mcplib.TaskStatusWorking)

	return taskHandle{}
}

// taskTestServer wires a server whose scale_service accepts the write but
// leaves the cluster unconverged, so the test controls when convergence
// happens.
func taskTestServer(t *testing.T, c *cache.Cache) *Server {
	t.Helper()

	accept := func(_ context.Context, id string) (swarm.Service, error) {
		return swarm.Service{ID: id}, nil
	}

	writeClient := &fakeWriteClient{
		scaleServiceFn: func(_ context.Context, id string, _ uint64) (swarm.Service, error) {
			return swarm.Service{ID: id}, nil
		},
		updateServiceImageFn: func(_ context.Context, id, _ string) (swarm.Service, error) {
			return swarm.Service{ID: id}, nil
		},
		rollbackServiceFn: accept,
		restartServiceFn:  accept,
	}

	return newToolTestServer(t, c, writeClient, config.OpsOperational)
}

// TestScaleServiceAsTaskCompletesOnConvergence is the headline behaviour: the
// call returns a handle at once, the task stays "working" while the cluster is
// still catching up, and completes only once the cache shows the replicas
// actually running.
//
// It runs through the real transport because that is the only place the
// behaviour exists: mcp-go creates the task, runs the handler on a goroutine
// whose context the HTTP server has already cancelled, and answers tasks/get
// from its own registry. None of that is reachable by calling the handler.
func TestScaleServiceAsTaskCompletesOnConvergence(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "web", 3 /* desired */, 1 /* running */)

	handler := taskTestServer(t, c).Handler()

	handle := callAsTask(t, handler, "scale_service", map[string]any{
		"id":       "web",
		"replicas": float64(3),
	})

	if handle.Task.Status != string(mcplib.TaskStatusWorking) {
		t.Fatalf("new task status = %q, want %q", handle.Task.Status, mcplib.TaskStatusWorking)
	}

	// Still catching up: the task must not have completed.
	time.Sleep(100 * time.Millisecond)

	if got := pollTask(
		t,
		handler,
		handle.Task.TaskId,
	); got.Task.Status != string(
		mcplib.TaskStatusWorking,
	) {
		t.Fatalf("task reported %q while only 1/3 replicas were running", got.Task.Status)
	}

	// Swarm converges.
	seedService(t, c, "web", 3, 3)

	final := awaitTerminal(t, handler, handle.Task.TaskId, 5*time.Second)
	if final.Task.Status != string(mcplib.TaskStatusCompleted) {
		t.Fatalf("task ended %q (%s), want %q",
			final.Task.Status, final.Task.StatusMessage, mcplib.TaskStatusCompleted)
	}
}

// TestScaleServiceStillWorksSynchronously is the regression guard for the trap
// in this phase: registering these tools through AddTaskTool would make
// mcp-go answer every plain tools/call with "does not support synchronous
// execution", breaking every client that does not use tasks.
func TestScaleServiceStillWorksSynchronously(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "web", 1, 1)

	handler := taskTestServer(t, c).Handler()

	_, envelope := mcpModern(t, handler, 1, "tools/call",
		`{"name":"scale_service","arguments":{"id":"web","replicas":1}}`)
	if envelope.Error != nil {
		t.Fatalf("synchronous scale_service failed: %+v", envelope.Error)
	}

	var result struct {
		IsError bool `json:"isError"`
	}

	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode tools/call result: %v", err)
	}

	if result.IsError {
		t.Fatalf("synchronous scale_service returned a tool error: %s", envelope.Result)
	}
}

// TestSynchronousCallDoesNotWaitForConvergence keeps the task path from
// leaking into the plain one. A tools/call with no task augmentation must
// return as soon as Docker accepts the change, even though the cluster is
// nowhere near the requested state.
func TestSynchronousCallDoesNotWaitForConvergence(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "web", 5, 0)

	handler := taskTestServer(t, c).Handler()

	done := make(chan struct{})

	go func() {
		defer close(done)

		mcpModern(t, handler, 1, "tools/call",
			`{"name":"scale_service","arguments":{"id":"web","replicas":5}}`)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("synchronous scale_service blocked on convergence")
	}
}

// taskToolArguments supplies a valid call for every tool that declares task
// support. A new task tool with no entry here fails the coverage check below
// rather than slipping through untested.
var taskToolArguments = map[string]map[string]any{
	"scale_service":        {"id": "web", "replicas": float64(2)},
	"update_service_image": {"id": "web", "image": "nginx:1.27"},
	"rollback_service":     {"id": "web"},
	"restart_service":      {"id": "web"},
}

// TestEveryTaskToolAwaitsConvergence holds the declaration and the behaviour
// together, tool by tool. A tool that advertises taskSupport but never calls
// awaitIfTask completes its task the instant Docker accepts the change — it
// tells an agent the cluster converged when it did not, which is precisely the
// failure this phase exists to prevent, and a test that only checks the task
// eventually completes would pass anyway.
//
// The check is that the task is still working while the cluster is not.
func TestEveryTaskToolAwaitsConvergence(t *testing.T) {
	srv := taskTestServer(t, cache.New(nil))

	declared := []string{}

	for _, td := range srv.registeredTools {
		if td.tool.Execution != nil && td.tool.Execution.TaskSupport == mcplib.TaskSupportOptional {
			declared = append(declared, td.tool.Name)
		}
	}

	if len(declared) == 0 {
		t.Fatal(
			"no tool declares taskSupport, so the Tasks extension advertises a capability no tool offers",
		)
	}

	for _, name := range declared {
		arguments, ok := taskToolArguments[name]
		if !ok {
			t.Errorf("tool %q declares taskSupport but has no case in taskToolArguments", name)

			continue
		}

		t.Run(name, func(t *testing.T) {
			// A fresh unconverged service per subtest: 2 replicas wanted,
			// none running.
			c := cache.New(nil)
			seedService(t, c, "web", 2, 0)

			handler := taskTestServer(t, c).Handler()

			handle := callAsTask(t, handler, name, arguments)

			time.Sleep(150 * time.Millisecond)

			got := pollTask(t, handler, handle.Task.TaskId)
			if got.Task.Status != string(mcplib.TaskStatusWorking) {
				t.Errorf("task for %q reported %q (%s) while 0/2 replicas were running; "+
					"the handler is not awaiting convergence",
					name, got.Task.Status, got.Task.StatusMessage)
			}
		})
	}
}

// TestTaskAugmentedCallStillEnforcesACL — a task must never become a way
// around the ACL check. The task path runs the very same handler as a plain
// call, so the check has to fire there too.
//
// This drives the handler directly rather than the transport: the ACL only
// engages when an identity is on the context, and the test transport
// authenticates nobody.
func TestTaskAugmentedCallStillEnforcesACL(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "web", 1, 1)

	scaled := false
	writeClient := &fakeWriteClient{
		scaleServiceFn: func(_ context.Context, id string, _ uint64) (swarm.Service, error) {
			scaled = true

			return swarm.Service{ID: id}, nil
		},
	}

	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:*"))

	srv := newToolTestServer(t, c, writeClient, config.OpsOperational,
		func(o *Options) { o.ACL = evaluator })

	td, ok := srv.findTool("scale_service")
	if !ok {
		t.Fatal("scale_service not registered")
	}

	request := newCallToolRequest("scale_service", map[string]any{
		"id":       "web",
		"replicas": float64(3),
	})
	request.Params.Task = &mcplib.TaskParams{}

	if _, err := td.handler(ctxWithIdentity(), request); err == nil {
		t.Error("task-augmented call was allowed despite a read-only policy")
	}

	if scaled {
		t.Error("ACL-denied task still performed the mutation")
	}
}

// TestFailedTaskReportsFailedNotCompleted pins the distinction an agent acts
// on. mcp-go marks a task completed whenever the handler returns no error, and
// our tool errors normally travel *inside* a successful result — so a refused
// mutation would show up as a completed task, and an agent polling tasks/get
// would carry on as though the cluster had changed.
func TestFailedTaskReportsFailedNotCompleted(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "web", 2, 2)

	// No writer stubbed: scale_service fails at the Docker call.
	srv := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsOperational)
	handler := srv.Handler()

	handle := callAsTask(t, handler, "scale_service", map[string]any{
		"id":       "web",
		"replicas": float64(3),
	})

	final := awaitTerminal(t, handler, handle.Task.TaskId, 5*time.Second)
	if final.Task.Status != string(mcplib.TaskStatusFailed) {
		t.Errorf("task for a refused mutation reported %q (%s), want %q",
			final.Task.Status, final.Task.StatusMessage, mcplib.TaskStatusFailed)
	}

	if final.Task.StatusMessage == "" {
		t.Error("failed task carries no status message explaining why")
	}
}

// TestServiceMutationResultIsCompact pins the shape a task retains. mcp-go
// keeps a completed task's result until its TTL elapses, and a client that
// omits task.ttl keeps it for the life of the process, so returning the whole
// swarm.Service here is what turns a busy agent into steady memory growth.
//
// Driven over the real transport so it covers the structured content a client
// actually receives, not just the handler's return value.
func TestServiceMutationResultIsCompact(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "web", 2, 2)

	handler := taskTestServer(t, c).Handler()

	_, envelope := mcpModern(t, handler, 1, "tools/call",
		`{"name":"scale_service","arguments":{"id":"web","replicas":2}}`)
	if envelope.Error != nil {
		t.Fatalf("scale_service failed: %+v", envelope.Error)
	}

	var result struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode tools/call result: %v", err)
	}

	if result.IsError {
		t.Fatalf("scale_service returned a tool error: %s", envelope.Result)
	}

	// A raw swarm.Service carries these; the compact result must not.
	for _, leaked := range []string{"TaskTemplate", "CreatedAt", "Endpoint", "PreviousSpec"} {
		if bytes.Contains(result.StructuredContent, []byte(leaked)) {
			t.Errorf(
				"result carries raw swarm.Service field %q: %s",
				leaked,
				result.StructuredContent,
			)
		}
	}

	var got serviceMutationResult
	if err := json.Unmarshal(result.StructuredContent, &got); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}

	if got.ID != "web" {
		t.Errorf("id = %q, want %q", got.ID, "web")
	}

	if got.Running != 2 {
		t.Errorf("running = %d, want 2 — the count is what convergence is judged on", got.Running)
	}

	if got.Mode != "replicated" {
		t.Errorf("mode = %q, want %q", got.Mode, "replicated")
	}
}
