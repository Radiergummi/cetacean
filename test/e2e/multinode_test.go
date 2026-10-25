//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

const (
	multiNodePort = 19028

	// taskPoll is the lane's docker.task_poll_interval, short so that a bound
	// built on it stays short.
	taskPoll = 2 * time.Second

	// pollBound is how long after the engine records a change the poll has to
	// carry it into the cache: one interval, plus the list and the reads.
	pollBound = taskPoll + 3*time.Second

	// eventBound is how long an event's re-list has to reach the cache.
	eventBound = 5 * time.Second

	// staleWindow is how long a cache with the poll off is watched not to
	// learn of a worker's task: longer than pollBound, so that a pass of the
	// first case is not one the second case would also allow.
	staleWindow = 2 * pollBound

	engineTimeout = 60 * time.Second
	observeEvery  = 100 * time.Millisecond
)

// TestMultiNode runs against a swarm of the manager and one worker. The
// manager's event stream carries no container events from the worker, so what
// happens to a task there reaches the cache only by the task poll, or by the
// re-list a service or node event triggers.
func TestMultiNode(t *testing.T) {
	if os.Getenv("CETACEAN_E2E_MULTINODE") == "" {
		t.Skip(
			"the multi-node lane is opt-in: set CETACEAN_E2E_MULTINODE=1, or run make test-multinode",
		)
	}

	env := harness.Up(t)
	env.SwarmInit(t)

	worker := env.JoinWorker(t)
	fixtures.LoadImage(t, worker.Docker)

	stack := fixtures.DeployStack(t, env, "multinode", []fixtures.ServiceSpec{{
		Name:        "crash",
		Replicas:    1,
		Command:     []string{"sleep infinity"},
		Constraints: []string{"node.role == worker"},
	}})
	service := stack + "_crash"

	t.Run("the poll reports a task that died on the worker", func(t *testing.T) {
		proc := startMultiNodeSUT(t, env, taskPoll.String())
		serviceID := serviceID(t, proc, service)

		victim := engineRunningTask(t, env, service, worker.NodeID, "")
		awaitCachedTaskState(t, proc, victim.ID, swarm.TaskStateRunning, pollBound)

		streamed := watchTaskFailure(t, proc, "/services/"+serviceID, victim.ID)

		failedAt := killTask(t, env, worker, victim)
		took := awaitCachedTaskState(t, proc, victim.ID, swarm.TaskStateFailed, pollBound)
		t.Logf("the cache reported the task failed %s after the engine did", took)

		if !closedBy(streamed, failedAt.Add(pollBound)) {
			t.Errorf("no task event on the service's stream reported %s failed", victim.ID)
		}

		replacement := engineRunningTask(t, env, service, worker.NodeID, victim.ID)
		awaitCachedTaskState(t, proc, replacement.ID, swarm.TaskStateRunning, pollBound)

		if running := cachedRunningTasks(t, proc, service); running != 1 {
			t.Errorf("RunningTasks for %s = %d, want 1 once the replacement runs", service, running)
		}
	})

	t.Run("without the poll, service and node events re-list the tasks", func(t *testing.T) {
		proc := startMultiNodeSUT(t, env, "0")

		victim := engineRunningTask(t, env, service, worker.NodeID, "")
		awaitCachedTaskState(t, proc, victim.ID, swarm.TaskStateRunning, eventBound)

		failedAt := killTask(t, env, worker, victim)

		// The control: were the failure to arrive by itself, the first case
		// would pass without the poll it is meant to pin.
		for time.Since(failedAt) < staleWindow {
			if state, _ := cachedTaskState(t, proc, victim.ID); state != swarm.TaskStateRunning {
				t.Fatalf(
					"with the poll off the cache read %s as %q %s after it died; something "+
						"reports worker containers to the manager, and this lane no longer "+
						"shows the poll is needed",
					victim.ID, state, time.Since(failedAt),
				)
			}

			time.Sleep(observeEvery * 5)
		}

		// A label changes no task, but the manager emits a service event.
		touchService(t, env, service, time.Now().Format(time.RFC3339Nano))
		awaitCachedTaskState(t, proc, victim.ID, swarm.TaskStateFailed, eventBound)

		next := engineRunningTask(t, env, service, worker.NodeID, victim.ID)
		killTask(t, env, worker, next)

		if state, _ := cachedTaskState(t, proc, next.ID); state == swarm.TaskStateFailed {
			t.Fatalf(
				"the cache read %s as failed before the node event that should re-list it",
				next.ID,
			)
		}

		touchNode(t, env, worker.NodeID, nil)
		awaitCachedTaskState(t, proc, next.ID, swarm.TaskStateFailed, eventBound)
	})

	t.Run("a global service's desired count honours its placement", func(t *testing.T) {
		proc := startMultiNodeSUT(t, env, "0")

		global := fixtures.DeployStack(t, env, "multinode-global", []fixtures.ServiceSpec{
			{
				Name:        "workers",
				Global:      true,
				Command:     []string{"sleep infinity"},
				Constraints: []string{"node.role == worker"},
			},
			{Name: "everywhere", Global: true, Command: []string{"sleep infinity"}},
		})

		// One task for the constrained service, one per node for the other.
		awaitDesiredTasks(t, proc, global, 3)

		t.Cleanup(func() {
			touchNode(t, env, worker.NodeID, func(spec *swarm.NodeSpec) {
				spec.Availability = swarm.NodeAvailabilityActive
			})
		})

		touchNode(t, env, worker.NodeID, func(spec *swarm.NodeSpec) {
			spec.Availability = swarm.NodeAvailabilityDrain
		})

		// A drained worker carries neither, and the manager only the second.
		awaitDesiredTasks(t, proc, global, 1)
	})
}

func startMultiNodeSUT(t *testing.T, env *harness.Env, poll string) *sut.Process {
	t.Helper()

	return sut.Start(t, sut.Config{
		Port:       multiNodePort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":                 "none",
			"CETACEAN_DOCKER_TASK_POLL_INTERVAL": poll,
		},
	})
}

// engineRunningTask waits for the manager to report a running task of the
// service on the node, other than the one excluded, and returns it.
func engineRunningTask(
	t *testing.T,
	env *harness.Env,
	service, nodeID, exclude string,
) swarm.Task {
	t.Helper()

	deadline := time.Now().Add(engineTimeout)

	for time.Now().Before(deadline) {
		tasks, err := env.Docker.TaskList(t.Context(), swarm.TaskListOptions{
			Filters: filters.NewArgs(
				filters.Arg("service", service),
				filters.Arg("node", nodeID),
				filters.Arg("desired-state", "running"),
			),
		})
		if err != nil {
			t.Fatalf("TaskList: %v", err)
		}

		for _, task := range tasks {
			if task.ID != exclude &&
				task.Status.State == swarm.TaskStateRunning &&
				task.Status.ContainerStatus != nil &&
				task.Status.ContainerStatus.ContainerID != "" {
				return task
			}
		}

		time.Sleep(observeEvery)
	}

	t.Fatalf("no running task of %s on node %s within %s", service, nodeID, engineTimeout)

	return swarm.Task{}
}

// killTask kills the task's container on the worker and returns when the
// manager first reported the task failed, which is what a bound counts from.
func killTask(t *testing.T, env *harness.Env, worker *harness.Worker, task swarm.Task) time.Time {
	t.Helper()

	containerID := task.Status.ContainerStatus.ContainerID
	if err := worker.Docker.ContainerKill(t.Context(), containerID, "KILL"); err != nil {
		t.Fatalf("ContainerKill %s: %v", containerID, err)
	}

	deadline := time.Now().Add(engineTimeout)

	for time.Now().Before(deadline) {
		inspected, _, err := env.Docker.TaskInspectWithRaw(t.Context(), task.ID)
		if err != nil {
			t.Fatalf("TaskInspect %s: %v", task.ID, err)
		}

		if inspected.Status.State == swarm.TaskStateFailed {
			return time.Now()
		}

		time.Sleep(observeEvery)
	}

	t.Fatalf("the manager did not report task %s failed within %s", task.ID, engineTimeout)

	return time.Time{}
}

// cachedTaskState reads a task's state from the SUT, reporting false for a
// task its cache does not hold.
func cachedTaskState(t *testing.T, proc *sut.Process, id string) (swarm.TaskState, bool) {
	t.Helper()

	var body struct {
		Task struct {
			Status struct {
				State swarm.TaskState `json:"State"`
			} `json:"Status"`
		} `json:"task"`
	}

	resp := getJSON(t, proc, "/tasks/"+id, &body) //nolint:bodyclose // closed in getJSON

	switch resp.StatusCode {
	case http.StatusOK:
		return body.Task.Status.State, true
	case http.StatusNotFound:
		return "", false
	default:
		t.Fatalf("GET /tasks/%s: status = %d", id, resp.StatusCode)

		return "", false
	}
}

// awaitCachedTaskState waits for the SUT to report the task in the state, and
// returns how long that took.
func awaitCachedTaskState(
	t *testing.T,
	proc *sut.Process,
	id string,
	want swarm.TaskState,
	within time.Duration,
) time.Duration {
	t.Helper()

	start := time.Now()

	var last swarm.TaskState

	for time.Since(start) < within {
		state, _ := cachedTaskState(t, proc, id)
		if state == want {
			return time.Since(start)
		}

		last = state

		time.Sleep(observeEvery)
	}

	t.Fatalf("task %s read %q, not %q, after %s", id, last, want, within)

	return 0
}

// watchTaskFailure subscribes to a resource's stream and closes the returned
// channel once a task event reports the task failed.
func watchTaskFailure(t *testing.T, proc *sut.Process, path, taskID string) <-chan struct{} {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proc.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "text/event-stream")

	resp, err := proc.StreamClient().Do(req)
	if err != nil {
		t.Fatalf("open stream %s: %v", path, err)
	}

	t.Cleanup(func() { resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s as a stream: status = %d", path, resp.StatusCode)
	}

	frames := make(chan sseFrame, 16)
	go readSSEFrames(resp.Body, frames, ctx.Done())

	failed := make(chan struct{})

	go func() {
		for frame := range frames {
			for _, event := range taskEventsIn(frame) {
				if event.ID == taskID && event.Resource.Status.State == swarm.TaskStateFailed {
					close(failed)

					return
				}
			}
		}
	}()

	return failed
}

// closedBy reports whether ch is closed by the deadline, checking it first so a
// deadline already past still sees a channel closed in time.
func closedBy(ch <-chan struct{}, deadline time.Time) bool {
	select {
	case <-ch:
		return true
	default:
	}

	select {
	case <-ch:
		return true
	case <-time.After(time.Until(deadline)):
		return false
	}
}

// sseTaskEvent is as much of a task event as the lane reads.
type sseTaskEvent struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Resource struct {
		Status struct {
			State swarm.TaskState `json:"State"`
		} `json:"Status"`
	} `json:"resource"`
}

// taskEventsIn decodes the task events a frame carries, alone or in a batch.
func taskEventsIn(frame sseFrame) []sseTaskEvent {
	var events []sseTaskEvent

	data := strings.TrimSpace(frame.data)

	switch frame.event {
	case "batch":
		if err := json.Unmarshal([]byte(data), &events); err != nil {
			return nil
		}
	case "task":
		var event sseTaskEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil
		}

		events = append(events, event)
	}

	tasks := events[:0]
	for _, event := range events {
		if event.Type == "task" {
			tasks = append(tasks, event)
		}
	}

	return tasks
}

// cachedRunningTasks reads a service's RunningTasks from the SUT's listing.
func cachedRunningTasks(t *testing.T, proc *sut.Process, name string) int {
	t.Helper()

	var body struct {
		Items []struct {
			Spec struct {
				Name string `json:"Name"`
			} `json:"Spec"`
			RunningTasks int `json:"RunningTasks"`
		} `json:"items"`
	}

	resp := getJSON(t, proc, "/services", &body) //nolint:bodyclose // closed in getJSON
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /services: status = %d", resp.StatusCode)
	}

	for _, item := range body.Items {
		if item.Spec.Name == name {
			return item.RunningTasks
		}
	}

	t.Fatalf("service %q not found in /services", name)

	return 0
}

// awaitDesiredTasks waits for the SUT's stack summary to expect want tasks.
func awaitDesiredTasks(t *testing.T, proc *sut.Process, stack string, want int) {
	t.Helper()

	var body struct {
		Items []struct {
			Name         string `json:"name"`
			DesiredTasks int    `json:"desiredTasks"`
		} `json:"items"`
	}

	got := -1
	start := time.Now()

	for time.Since(start) < eventBound {
		resp := getJSON(t, proc, "/stacks/summary", &body) //nolint:bodyclose // closed in getJSON
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /stacks/summary: status = %d", resp.StatusCode)
		}

		for _, item := range body.Items {
			if item.Name == stack {
				got = item.DesiredTasks
			}
		}

		if got == want {
			return
		}

		time.Sleep(observeEvery)
	}

	t.Fatalf("stack %s expects %d tasks, want %d", stack, got, want)
}

// touchNode applies change to the node's spec, or only labels it for a nil
// change; either makes the manager emit a node event.
func touchNode(t *testing.T, env *harness.Env, id string, change func(*swarm.NodeSpec)) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), engineTimeout)
	defer cancel()

	node, _, err := env.Docker.NodeInspectWithRaw(ctx, id)
	if err != nil {
		t.Fatalf("NodeInspect %s: %v", id, err)
	}

	if change == nil {
		if node.Spec.Labels == nil {
			node.Spec.Labels = map[string]string{}
		}

		node.Spec.Labels["e2e.touched"] = time.Now().Format(time.RFC3339Nano)
	} else {
		change(&node.Spec)
	}

	if err := env.Docker.NodeUpdate(ctx, id, node.Version, node.Spec); err != nil {
		t.Fatalf("NodeUpdate %s: %v", id, err)
	}
}
