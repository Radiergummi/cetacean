//go:build e2e

package harness

import (
	"context"
	"fmt"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

const (
	workerHost    = "tcp://127.0.0.1:12376"
	workerService = "dind-worker"
	workerProfile = "multinode"
	workerLabel   = "cetacean-e2e-worker=true"

	// managerAddr is the manager as the worker reaches it, over the compose
	// network; the swarm's advertised 127.0.0.1 is the worker's own loopback.
	managerAddr = "dind:2377"

	nodeStateTimeout = 60 * time.Second
)

// Worker is a second engine joined to the environment's swarm as a worker.
type Worker struct {
	DockerHost string
	Docker     *client.Client
	NodeID     string
}

// JoinWorker starts the worker engine and joins it to the swarm. At cleanup it
// leaves, its node is removed and its container stopped, so the lanes that run
// afterwards see the single-node swarm they were written against.
func (e *Env) JoinWorker(t *testing.T) *Worker {
	t.Helper()

	upCtx, cancel := context.WithTimeout(t.Context(), upTimeout)
	defer cancel()

	if err := compose(upCtx, "--profile", workerProfile, "up", "-d", workerService); err != nil {
		t.Fatalf("%v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), upTimeout)
		defer cancel()

		if err := compose(ctx, "--profile", workerProfile, "rm", "-sf", workerService); err != nil {
			t.Errorf("%v", err)
		}
	})

	docker, err := client.NewClientWithOpts(
		client.WithHost(workerHost),
		client.WithVersion("1.46"),
	)
	if err != nil {
		t.Fatalf("worker docker client: %v", err)
	}

	if err := waitForEngine(docker, workerHost, workerLabel); err != nil {
		t.Fatalf("%v", err)
	}

	worker := &Worker{DockerHost: workerHost, Docker: docker}

	if err := e.join(t.Context(), worker); err != nil {
		t.Fatalf("%v", err)
	}

	t.Cleanup(func() { e.leave(t, worker) })

	return worker
}

// join makes the worker a member, first shedding any membership a run that
// never reached its cleanup left in the worker's volume and the manager's
// node list.
func (e *Env) join(ctx context.Context, worker *Worker) error {
	info, err := worker.Docker.Info(ctx)
	if err != nil {
		return fmt.Errorf("worker info: %w", err)
	}

	if info.Swarm.LocalNodeState != swarm.LocalNodeStateInactive {
		if err := worker.Docker.SwarmLeave(ctx, true); err != nil {
			return fmt.Errorf("worker leaving a stale swarm: %w", err)
		}

		if err := e.awaitNodeState(ctx, info.Swarm.NodeID, swarm.NodeStateDown); err != nil {
			return err
		}
	}

	if err := e.removeDownWorkers(ctx); err != nil {
		return err
	}

	cluster, err := e.Docker.SwarmInspect(ctx)
	if err != nil {
		return fmt.Errorf("SwarmInspect: %w", err)
	}

	if err := worker.Docker.SwarmJoin(ctx, swarm.JoinRequest{
		ListenAddr:    "0.0.0.0:2377",
		AdvertiseAddr: "eth0",
		RemoteAddrs:   []string{managerAddr},
		JoinToken:     cluster.JoinTokens.Worker,
	}); err != nil {
		return fmt.Errorf("SwarmJoin: %w", err)
	}

	info, err = worker.Docker.Info(ctx)
	if err != nil {
		return fmt.Errorf("worker info: %w", err)
	}

	worker.NodeID = info.Swarm.NodeID

	return e.awaitNodeState(ctx, worker.NodeID, swarm.NodeStateReady)
}

// leave takes the worker out of the swarm and its node off the manager's list.
// It runs from t.Cleanup, where t.Context() is already canceled.
func (e *Env) leave(t *testing.T, worker *Worker) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), nodeStateTimeout)
	defer cancel()

	if err := worker.Docker.SwarmLeave(ctx, true); err != nil {
		t.Errorf("worker SwarmLeave: %v", err)
	}

	if err := e.awaitNodeState(ctx, worker.NodeID, swarm.NodeStateDown); err != nil {
		t.Errorf("%v", err)
	}

	if err := e.Docker.NodeRemove(ctx, worker.NodeID, swarm.NodeRemoveOptions{}); err != nil {
		t.Errorf("NodeRemove %s: %v", worker.NodeID, err)
	}
}

// removeDownWorkers removes every worker node the manager reports down. This
// swarm has no workers but the ones this harness joins.
func (e *Env) removeDownWorkers(ctx context.Context) error {
	nodes, err := e.Docker.NodeList(ctx, swarm.NodeListOptions{
		Filters: filters.NewArgs(filters.Arg("role", string(swarm.NodeRoleWorker))),
	})
	if err != nil {
		return fmt.Errorf("NodeList: %w", err)
	}

	for _, node := range nodes {
		if node.Status.State != swarm.NodeStateDown {
			continue
		}

		if err := e.Docker.NodeRemove(ctx, node.ID, swarm.NodeRemoveOptions{}); err != nil {
			return fmt.Errorf("NodeRemove %s: %w", node.ID, err)
		}
	}

	return nil
}

// awaitNodeState waits for the manager to report the node in the state. A node
// it does not know counts as down: its swarm may predate the manager's.
func (e *Env) awaitNodeState(ctx context.Context, id string, want swarm.NodeState) error {
	deadline := time.Now().Add(nodeStateTimeout)

	var last swarm.NodeState

	for time.Now().Before(deadline) {
		node, _, err := e.Docker.NodeInspectWithRaw(ctx, id)
		if err == nil && node.Status.State == want {
			return nil
		}

		if cerrdefs.IsNotFound(err) && want == swarm.NodeStateDown {
			return nil
		}

		last = node.Status.State

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("node %s is %q, not %q, after %s", id, last, want, nodeStateTimeout)
}
