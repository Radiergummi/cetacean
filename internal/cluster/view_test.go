package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

// A row must answer "what is this and is it healthy" without a second call:
// that is the whole reason the raw Docker object is not returned.
func TestRowsForServicesCarryDerivedState(t *testing.T) {
	svc := replicated("api", 3)
	svc.Spec.Labels = map[string]string{"com.docker.stack.namespace": "demo"}

	tasks := []swarm.Task{
		{ID: "t1", ServiceID: "svc-api", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
		{ID: "t2", ServiceID: "svc-api", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
	}

	rows := RowsForServices([]swarm.Service{svc}, tasks)

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}

	got := rows[0]

	if got.ID != "svc-api" || got.Name != "api" {
		t.Errorf("identity = %q/%q, want svc-api/api", got.ID, got.Name)
	}
	if got.Type != "service" {
		t.Errorf("type = %q, want service", got.Type)
	}
	if got.Stack != "demo" {
		t.Errorf("stack = %q, want demo", got.Stack)
	}
	if got.Detail != "api:1" {
		t.Errorf("detail = %q, want the image without its digest", got.Detail)
	}
	if got.Desired != 3 || got.Running != 2 {
		t.Errorf("desired/running = %d/%d, want 3/2", got.Desired, got.Running)
	}
	if got.State != "pending" {
		t.Errorf("state = %q, want pending for 2 of 3 replicas", got.State)
	}
}

// Each builder must produce a row carrying an identity and a type, or find()
// has a hole for that resource. The assertion is deliberately shallow — the
// per-type detail is covered where it is interesting — and this exists to catch
// a type nobody wrote a builder for. Services are covered above.
func TestBuildersProduceIdentifiableRows(t *testing.T) {
	cases := []struct {
		name     string
		rows     []Row
		wantType string
		extra    func(t *testing.T, row Row)
	}{
		{"nodes", RowsForNodes([]swarm.Node{{
			ID:          "n1",
			Description: swarm.NodeDescription{Hostname: "worker-a"},
			Spec:        swarm.NodeSpec{Role: swarm.NodeRoleWorker},
			Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		}}), "node", nil},

		{"tasks", RowsForTasks(
			[]swarm.Task{{ID: "t1", ServiceID: "svc-api", NodeID: "n1",
				Status: swarm.TaskStatus{State: swarm.TaskStateRunning}}},
			[]swarm.Service{replicated("api", 1)},
			[]swarm.Node{{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-a"}}},
		), "task", nil},

		{"configs", RowsForConfigs([]swarm.Config{{
			ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "conf"}},
		}}), "config", nil},

		{"secrets", RowsForSecrets([]swarm.Secret{{
			ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}},
		}}), "secret", nil},

		{
			"networks",
			RowsForNetworks([]network.Summary{overlay("net1", "demo_overlay")}),
			"network",
			nil,
		},

		{
			"volumes",
			RowsForVolumes([]*volume.Volume{{Name: "data", Driver: "local"}}),
			"volume",
			func(t *testing.T, row Row) {
				t.Helper()

				if row.ID != row.Name {
					t.Errorf(
						"id/name = %q/%q, want equal: a volume is keyed by name",
						row.ID,
						row.Name,
					)
				}
			},
		},

		{
			"stacks",
			RowsForStacks([]cache.Stack{{Name: "demo", Services: []string{"svc-api", "svc-db"}}}),
			"stack",
			func(t *testing.T, row Row) {
				t.Helper()

				if row.Desired != 2 {
					t.Errorf("desired = %d, want 2 (number of services in the stack)", row.Desired)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(tc.rows))
			}

			got := tc.rows[0]

			if got.Type != tc.wantType {
				t.Errorf("type = %q, want %q", got.Type, tc.wantType)
			}
			if got.ID == "" || got.Name == "" {
				t.Errorf("row = %+v, want both an id and a name", got)
			}
			if tc.extra != nil {
				tc.extra(t, got)
			}
		})
	}
}

// The reason is the point of a digest. "state: failed" is not an answer; the
// unmet constraint is. Swarm puts it in Status.Err, which is where this reads.
func TestServiceDigestExplainsWhyAServiceIsNotRunning(t *testing.T) {
	svc := replicated("stuck", 1)
	svc.Spec.TaskTemplate.Placement = &swarm.Placement{
		Constraints: []string{"node.labels.gpu == true"},
	}

	tasks := []swarm.Task{{
		ID:           "t1",
		ServiceID:    "svc-stuck",
		DesiredState: swarm.TaskStateRunning,
		Status: swarm.TaskStatus{
			State:   swarm.TaskStatePending,
			Message: "pending task scheduling",
			Err:     "no suitable node (scheduling constraints not satisfied on 1 node)",
		},
	}}

	got := ServiceDigest(svc, tasks, nil)

	if got.Reason != "no suitable node (scheduling constraints not satisfied on 1 node)" {
		t.Errorf("reason = %q, want Status.Err verbatim", got.Reason)
	}
	if got.State == "running" {
		t.Errorf("state = %q, want a non-running state", got.State)
	}
	if len(got.RecentFailures) != 1 {
		t.Fatalf("recentFailures = %d, want the pending task", len(got.RecentFailures))
	}
}

// A healthy service has nothing to explain, and a reason on a healthy resource
// would read as though something were wrong.
func TestServiceDigestOmitsReasonWhenHealthy(t *testing.T) {
	svc := replicated("api", 1)
	tasks := []swarm.Task{{
		ID: "t1", ServiceID: "svc-api", DesiredState: swarm.TaskStateRunning,
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
	}}

	got := ServiceDigest(svc, tasks, nil)

	if got.Reason != "" {
		t.Errorf("reason = %q, want empty for a converged service", got.Reason)
	}
}
