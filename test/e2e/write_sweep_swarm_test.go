//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// Cluster-level write drivers for the write sweep: the three reversible
// /swarm/* tuning patches and the join-token rotation.
//
// Unlike every other driver in this sweep, these mutate the one thing all the
// lanes in this test binary share — the swarm itself. There is no throwaway
// fixture to work on. Each therefore reads the current value, changes it to
// something it can recognise, verifies the change reached the engine, and
// restores what was there, so a lane that runs afterwards finds the cluster it
// expected. `PATCH /swarm/encryption` stays permanently excused for exactly
// the reason the others do not: enabling autolock means a manager restart
// needs an unlock key, and the harness restarts SUTs but has nowhere to keep
// one.

// swarmSpec reads the live swarm spec off the engine.
func swarmSpec(t *testing.T, env *harness.Env) swarm.Spec {
	t.Helper()

	cluster, err := env.Docker.SwarmInspect(context.Background())
	if err != nil {
		t.Fatalf("SwarmInspect: %v", err)
	}

	return cluster.Spec
}

// restoreSwarmSpec puts a spec back, re-reading the version first because the
// mutation under test has moved it on. Registered as a cleanup by each driver
// below.
func restoreSwarmSpec(t *testing.T, env *harness.Env, spec swarm.Spec) {
	t.Helper()

	t.Cleanup(func() {
		current, err := env.Docker.SwarmInspect(context.Background())
		if err != nil {
			t.Errorf("cleanup: SwarmInspect: %v", err)

			return
		}

		if err := env.Docker.SwarmUpdate(
			context.Background(), current.Version, spec, swarm.UpdateFlags{},
		); err != nil {
			t.Errorf("cleanup: SwarmUpdate: %v", err)
		}
	})
}

func driveSwarmOrchestration(t *testing.T, env *harness.Env, proc *sut.Process) {
	before := swarmSpec(t, env)
	restoreSwarmSpec(t, env, before)

	// Chosen to differ from whatever is configured, so the assertion cannot
	// pass on a no-op.
	want := int64(7)
	if before.Orchestration.TaskHistoryRetentionLimit != nil &&
		*before.Orchestration.TaskHistoryRetentionLimit == want {
		want = 9
	}

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/swarm/orchestration",
		"application/merge-patch+json",
		fmt.Appendf(nil, `{"TaskHistoryRetentionLimit":%d}`, want),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /swarm/orchestration: status = %d, want 200", resp.StatusCode)
	}

	after := swarmSpec(t, env)

	if after.Orchestration.TaskHistoryRetentionLimit == nil ||
		*after.Orchestration.TaskHistoryRetentionLimit != want {
		t.Errorf("engine TaskHistoryRetentionLimit = %v, want %d",
			after.Orchestration.TaskHistoryRetentionLimit, want)
	}
}

func driveSwarmRaft(t *testing.T, env *harness.Env, proc *sut.Process) {
	before := swarmSpec(t, env)
	restoreSwarmSpec(t, env, before)

	want := before.Raft.SnapshotInterval + 1000

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/swarm/raft",
		"application/merge-patch+json",
		fmt.Appendf(nil, `{"SnapshotInterval":%d}`, want),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /swarm/raft: status = %d, want 200", resp.StatusCode)
	}

	after := swarmSpec(t, env)

	if after.Raft.SnapshotInterval != want {
		t.Errorf("engine SnapshotInterval = %d, want %d", after.Raft.SnapshotInterval, want)
	}

	// The handler copies the whole spec and overwrites only the fields the
	// patch named. A patch that replaced swarm.RaftConfig wholesale would
	// zero the rest and still answer 200 — and an election tick of zero is
	// not something an operator finds out about until a leader fails.
	if after.Raft.ElectionTick != before.Raft.ElectionTick {
		t.Errorf("engine ElectionTick = %d, was %d; the patch replaced the raft config "+
			"rather than merging into it",
			after.Raft.ElectionTick, before.Raft.ElectionTick)
	}
}

func driveSwarmDispatcher(t *testing.T, env *harness.Env, proc *sut.Process) {
	before := swarmSpec(t, env)
	restoreSwarmSpec(t, env, before)

	want := before.Dispatcher.HeartbeatPeriod + time.Second

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/swarm/dispatcher",
		"application/merge-patch+json",
		fmt.Appendf(nil, `{"HeartbeatPeriod":%d}`, want),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /swarm/dispatcher: status = %d, want 200", resp.StatusCode)
	}

	after := swarmSpec(t, env)

	if after.Dispatcher.HeartbeatPeriod != want {
		t.Errorf("engine HeartbeatPeriod = %s, want %s",
			after.Dispatcher.HeartbeatPeriod, want)
	}
}

func driveSwarmRotateToken(t *testing.T, env *harness.Env, proc *sut.Process) {
	// Safe on this environment for a reason worth stating: the swarm has one
	// node and nothing ever joins it, so invalidating the worker join token
	// costs nothing. It is not restored — a join token has no "previous
	// value" to put back, and nothing reads it.
	before, err := env.Docker.SwarmInspect(context.Background())
	if err != nil {
		t.Fatalf("SwarmInspect: %v", err)
	}

	resp := sweepRequest(
		t, proc, http.MethodPost, "/swarm/rotate-token",
		"application/json", []byte(`{"target":"worker"}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /swarm/rotate-token: status = %d, want 204", resp.StatusCode)
	}

	after, err := env.Docker.SwarmInspect(context.Background())
	if err != nil {
		t.Fatalf("SwarmInspect after rotation: %v", err)
	}

	if after.JoinTokens.Worker == before.JoinTokens.Worker {
		t.Error("the worker join token is unchanged after a rotation that answered 204")
	}

	// Rotating one token must not rotate the other: an operator revoking
	// worker access would otherwise silently invalidate every manager's
	// join token too.
	if after.JoinTokens.Manager != before.JoinTokens.Manager {
		t.Error("rotating the worker token also rotated the manager token")
	}
}

// TestResyncIsAuthenticatedAndGated drives `POST /-/resync`, the one route
// under the `/-/` prefix that does work on request rather than reporting
// state: each call is a full seven-goroutine sweep of the Docker API,
// unbounded and unthrottled, so an uncredentialed caller able to reach the
// port could amplify one cheap request into a cluster enumeration at will.
//
// It is the one `/-/` path internal/auth's isExempt does not exempt, and it
// additionally requires the caller to hold a grant. It is deliberately not
// gated on the operations level: that says what a deployment may do to the
// cluster, and a resync only re-reads it, so a read-only deployment keeps the
// dashboard's refresh button.
func TestResyncIsAuthenticatedAndGated(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	// An auth mode that refuses an uncredentialed caller on every non-exempt
	// route, at operations level 0 — so a 200 here would mean the route is
	// reachable by anyone, in the most locked-down configuration there is.
	policy := filepath.Join(t.TempDir(), "acl.yaml")
	if err := os.WriteFile(policy, []byte(sseACLPolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	proc := sut.Start(t, sut.Config{
		Port:       writeSweepPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":            "headers",
			"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
			"CETACEAN_AUTH_HEADERS_GROUPS":  "X-Auth-Groups",
			"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
			"CETACEAN_ACL_POLICY_FILE":      policy,
			"CETACEAN_OPERATIONS_LEVEL":     "0",
		},
	})

	resp := sweepRequest(t, proc, http.MethodPost, "/-/resync", "application/json", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf(
			"POST /-/resync with no credentials against a headers-auth SUT at operations "+
				"level 0: status = %d, want 401 or 403; body: %s",
			resp.StatusCode, body,
		)
	}
}
