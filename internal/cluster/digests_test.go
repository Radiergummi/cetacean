package cluster

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

// A drained node still reports "ready" in Status.State while Swarm evacuates
// it, but the scheduler will not place anything there — Availability is the
// field that actually answers "can this take work", and this is the rule most
// likely to be got wrong.
func TestNodeDigestReportsDrainOverReadyState(t *testing.T) {
	node := swarm.Node{
		ID:          "n1",
		Description: swarm.NodeDescription{Hostname: "worker-a"},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityDrain,
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
	}

	got := NodeDigest(node, nil, nil)

	if got.State != "drain" {
		t.Errorf("state = %q, want drain (not the underlying ready status)", got.State)
	}
}

// Two live tasks belonging to the same service must collapse into one
// related entry — a caller asking "what runs here" wants distinct services,
// not one row per replica.
func TestNodeDigestDedupsRelatedServices(t *testing.T) {
	node := swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-a"}}

	tasks := []swarm.Task{
		{ID: "t1", ServiceID: "svc-api", NodeID: "n1", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
		{ID: "t2", ServiceID: "svc-api", NodeID: "n1", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
	}

	got := NodeDigest(node, tasks, []swarm.Service{replicated("api", 2)})

	if len(got.Related) != 1 {
		t.Fatalf("related = %d, want 1 (one service, two replicas)", len(got.Related))
	}
	if got.Related[0].Type != "service" || got.Related[0].Relation != "runs" {
		t.Errorf("related[0] = %+v, want type service / relation runs", got.Related[0])
	}
	if got.Related[0].Name != "api" {
		t.Errorf("related[0].name = %q, want api", got.Related[0].Name)
	}
}

// A shutdown-bound task is history, not a current occupant of the node — it
// must not appear in Related even though its NodeID still matches.
func TestNodeDigestRelatedExcludesShutdownTasks(t *testing.T) {
	node := swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-a"}}

	tasks := []swarm.Task{
		{ID: "t1", ServiceID: "svc-api", NodeID: "n1", DesiredState: swarm.TaskStateShutdown,
			Status: swarm.TaskStatus{State: swarm.TaskStateShutdown}},
	}

	got := NodeDigest(node, tasks, []swarm.Service{replicated("api", 1)})

	if len(got.Related) != 0 {
		t.Errorf(
			"related = %d, want 0 for a task the orchestrator has already replaced",
			len(got.Related),
		)
	}
}

// exitCode is only meaningful once a task has actually stopped; reporting it
// for a running task would show Docker's placeholder -1 as though it meant
// something.
func TestTaskDigestExitCodeOnlyForTerminalTasks(t *testing.T) {
	running := swarm.Task{
		ID: "t1", ServiceID: "svc-api", NodeID: "n1",
		Status: swarm.TaskStatus{
			State:           swarm.TaskStateRunning,
			ContainerStatus: &swarm.ContainerStatus{ExitCode: -1, ContainerID: "c1"},
		},
	}

	got := TaskDigest(running, nil, nil)
	if _, ok := got.Details["exitCode"]; ok {
		t.Errorf("exitCode present = %v, want omitted for a running task", got.Details["exitCode"])
	}

	failed := swarm.Task{
		ID: "t2", ServiceID: "svc-api", NodeID: "n1",
		Status: swarm.TaskStatus{
			State:           swarm.TaskStateFailed,
			ContainerStatus: &swarm.ContainerStatus{ExitCode: 137, ContainerID: "c2"},
		},
	}

	got = TaskDigest(failed, nil, nil)
	if got.Details["exitCode"] != 137 {
		t.Errorf("exitCode = %v, want 137 for a failed task", got.Details["exitCode"])
	}
}

// A caller may read a task without permission to read its service — the
// digest must still be usable: a name derived from the task's own ID, and a
// related entry carrying the raw ID rather than an empty name.
func TestTaskDigestHandlesNilService(t *testing.T) {
	task := swarm.Task{
		ID: "t1", ServiceID: "svc-api", NodeID: "n1",
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
	}

	got := TaskDigest(task, nil, nil)

	if got.Name != "t1" {
		t.Errorf("name = %q, want the task ID as a fallback", got.Name)
	}

	var instanceOf *Related
	for i := range got.Related {
		if got.Related[i].Relation == "instance-of" {
			instanceOf = &got.Related[i]
		}
	}
	if instanceOf == nil {
		t.Fatalf("related = %+v, want an instance-of entry even without the service", got.Related)
	}
	if instanceOf.ID != "svc-api" || instanceOf.Name != "svc-api" {
		t.Errorf("instance-of = %+v, want id/name both svc-api", instanceOf)
	}
}

// A stack with one failed and one running service must report the worse
// state and name the offending service — a stack has no Swarm status of its
// own to draw a reason from.
func TestStackDigestReportsWorstMemberState(t *testing.T) {
	ok := replicated("ok", 1)
	stuck := replicated("stuck", 1)

	tasks := []swarm.Task{
		{ID: "t1", ServiceID: ok.ID, DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
	}

	stack := cache.StackDetail{Name: "demo", Services: []swarm.Service{ok, stuck}}

	got := StackDigest(stack, tasks)

	if got.State != "failed" {
		t.Fatalf("state = %q, want failed", got.State)
	}
	if got.Reason != "service stuck is failed" {
		t.Errorf("reason = %q, want it to name the failed service", got.Reason)
	}
}

// A stack holding no services has nothing wrong with it.
func TestStackDigestEmptyStackIsRunning(t *testing.T) {
	got := StackDigest(cache.StackDetail{Name: "empty"}, nil)

	if got.State != "running" {
		t.Errorf("state = %q, want running for a stack with no services", got.State)
	}
	if got.Reason != "" {
		t.Errorf("reason = %q, want empty", got.Reason)
	}
}

// sizeBytes is the one thing a config's digest reports about its payload —
// the length, never the content.
func TestConfigDigestReportsSizeBytes(t *testing.T) {
	cfg := swarm.Config{
		ID:   "c1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "conf"}, Data: []byte("hello")},
	}

	got := ConfigDigest(cfg, nil)

	if got.Details["sizeBytes"] != 5 {
		t.Errorf("sizeBytes = %v, want 5", got.Details["sizeBytes"])
	}
}

// A secret's digest must never leak its data, not even accidentally through
// some other field's formatting — the same style as ServiceDetails' env-leak
// test.
func TestSecretDigestNeverLeaksData(t *testing.T) {
	sec := swarm.Secret{
		ID: "s1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "sec"},
			Data:        []byte("hunter2"),
		},
	}

	users := []cache.ServiceRef{{ID: "svc-api", Name: "api"}}

	got := SecretDigest(sec, users)

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "hunter2") {
		t.Fatalf("json = %s, want no trace of the secret's data", data)
	}
	if _, ok := got.Details["sizeBytes"]; ok {
		t.Errorf(
			"sizeBytes present = %v, want a secret to report no size at all",
			got.Details["sizeBytes"],
		)
	}

	if len(got.Related) != 1 || got.Related[0].Relation != "used-by" {
		t.Errorf("related = %+v, want one used-by entry", got.Related)
	}
}

// Subnets come from IPAM.Config, which carries only an IP block per entry —
// this is the one field that makes the network's own address space visible.
func TestNetworkDigestReportsSubnets(t *testing.T) {
	net := overlay("net1", "demo_overlay")
	net.IPAM = network.IPAM{Config: []network.IPAMConfig{{Subnet: "10.0.0.0/24"}}}

	got := NetworkDigest(net, nil)

	subnets, ok := got.Details["subnets"].([]string)
	if !ok {
		t.Fatalf("subnets = %T, want []string", got.Details["subnets"])
	}
	if len(subnets) != 1 || subnets[0] != "10.0.0.0/24" {
		t.Errorf("subnets = %v, want [10.0.0.0/24]", subnets)
	}
}

// A volume mounted by two services must produce two mounted-by entries, one
// per user, so a caller can find every consumer without a second search.
func TestVolumeDigestReportsMountedByUsers(t *testing.T) {
	vol := volume.Volume{Name: "data", Driver: "local"}
	users := []cache.ServiceRef{
		{ID: "svc-api", Name: "api"},
		{ID: "svc-worker", Name: "worker"},
	}

	got := VolumeDigest(vol, users)

	if len(got.Related) != 2 {
		t.Fatalf("related = %d, want 2", len(got.Related))
	}
	for _, r := range got.Related {
		if r.Relation != "mounted-by" || r.Type != "service" {
			t.Errorf("related entry = %+v, want type service / relation mounted-by", r)
		}
	}
}

// No digest builder may marshal related or recentFailures as null: a client
// reading the JSON output schema should never have to special-case an absent
// array.
func TestDigestBuildersMarshalEmptySlicesNotNull(t *testing.T) {
	digests := map[string]Digest{
		"node":    NodeDigest(swarm.Node{ID: "n1"}, nil, nil),
		"task":    TaskDigest(swarm.Task{ID: "t1"}, nil, nil),
		"stack":   StackDigest(cache.StackDetail{Name: "empty"}, nil),
		"config":  ConfigDigest(swarm.Config{ID: "c1"}, nil),
		"secret":  SecretDigest(swarm.Secret{ID: "s1"}, nil),
		"network": NetworkDigest(network.Summary{ID: "net1"}, nil),
		"volume":  VolumeDigest(volume.Volume{Name: "data"}, nil),
	}

	for name, digest := range digests {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(digest)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			if !strings.Contains(string(data), `"recentFailures":[]`) {
				t.Errorf("json = %s, want recentFailures as an empty array, not null", data)
			}
			if !strings.Contains(string(data), `"related":[]`) {
				t.Errorf("json = %s, want related as an empty array, not null", data)
			}
		})
	}
}
