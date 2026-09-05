package cluster

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

// A node that is both unreachable and drained must report the strictly worse
// fact — down beats drain — and Details must still carry both raw values, so
// neither is lost to the one that becomes the headline State.
func TestNodeDigestDownBeatsDrain(t *testing.T) {
	node := swarm.Node{
		ID:          "n1",
		Description: swarm.NodeDescription{Hostname: "worker-a"},
		Spec:        swarm.NodeSpec{Availability: swarm.NodeAvailabilityDrain},
		Status:      swarm.NodeStatus{State: swarm.NodeStateDown},
	}

	got := NodeDigest(node, nil, nil)

	if got.State != "down" {
		t.Errorf("state = %q, want down (unreachable beats drained)", got.State)
	}
	if got.Details["status"] != "down" {
		t.Errorf("details[status] = %v, want down", got.Details["status"])
	}
	if got.Details["availability"] != "drain" {
		t.Errorf("details[availability] = %v, want drain", got.Details["availability"])
	}
}

// RowsForNodes and NodeDigest must never disagree about the same node's
// state: this is the exact divergence DeriveNodeState was extracted to
// prevent.
// A task's name was being derived three different ways in this package before
// TaskName: a list row named a task after its service alone, so every replica
// rendered identically, while the digest and the search results named the same
// record "<service>.<slot>". One tool answering two ways for one record is the
// drift this package exists to prevent, so the list and the detail are held
// against each other the way the node state already is.
func TestRowsForTasksAgreesWithTaskDigest(t *testing.T) {
	replicatedService := replicated("api", 2)
	globalService := swarm.Service{
		ID: "svc-agent",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "agent"},
			Mode:        swarm.ServiceMode{Global: &swarm.GlobalService{}},
		},
	}

	cases := []struct {
		name    string
		task    swarm.Task
		service swarm.Service
	}{
		{
			"replicated slot 1",
			swarm.Task{ID: "t1", ServiceID: "svc-api", Slot: 1},
			replicatedService,
		},
		{
			"replicated slot 2",
			swarm.Task{ID: "t2", ServiceID: "svc-api", Slot: 2},
			replicatedService,
		},
		{
			"global on a node",
			swarm.Task{ID: "t3", ServiceID: "svc-agent", NodeID: "n1"},
			globalService,
		},
		{
			"global not yet assigned",
			swarm.Task{ID: "t4", ServiceID: "svc-agent"},
			globalService,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := RowsForTasks(
				[]swarm.Task{tc.task},
				[]swarm.Service{tc.service},
				nil,
			)[0]
			digest := TaskDigest(tc.task, &tc.service, nil)

			if row.Name != digest.Name {
				t.Errorf(
					"row.Name = %q, digest.Name = %q, want them equal",
					row.Name,
					digest.Name,
				)
			}
		})
	}
}

// Two replicas of one service must not render as two identical rows — the
// whole point of a list is telling them apart.
func TestRowsForTasksDistinguishesReplicas(t *testing.T) {
	svc := replicated("api", 2)
	rows := RowsForTasks(
		[]swarm.Task{
			{ID: "t1", ServiceID: "svc-api", Slot: 1},
			{ID: "t2", ServiceID: "svc-api", Slot: 2},
		},
		[]swarm.Service{svc},
		nil,
	)

	if rows[0].Name == rows[1].Name {
		t.Errorf("both replicas rendered as %q", rows[0].Name)
	}
}

func TestRowsForNodesAgreesWithNodeDigest(t *testing.T) {
	cases := []swarm.Node{
		{ID: "n1", Status: swarm.NodeStatus{State: swarm.NodeStateReady}},
		{
			ID:     "n2",
			Spec:   swarm.NodeSpec{Availability: swarm.NodeAvailabilityDrain},
			Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		},
		{ID: "n3", Status: swarm.NodeStatus{State: swarm.NodeStateDown}},
		{
			ID:     "n4",
			Spec:   swarm.NodeSpec{Availability: swarm.NodeAvailabilityDrain},
			Status: swarm.NodeStatus{State: swarm.NodeStateDown},
		},
	}

	for _, node := range cases {
		row := RowsForNodes([]swarm.Node{node})[0]
		digest := NodeDigest(node, nil, nil)

		if row.State != digest.State {
			t.Errorf(
				"node %s: row.State = %q, digest.State = %q, want them equal",
				node.ID,
				row.State,
				digest.State,
			)
		}
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

// Docker's own naming convention: a replicated task is named by its slot, a
// global one by the node it landed on, since a global service has no slot to
// distinguish its replicas by.
func TestTaskDigestNamesReplicatedAndGlobalTasks(t *testing.T) {
	svc := replicated("api", 3)

	replicatedTask := swarm.Task{ID: "t1", ServiceID: svc.ID, Slot: 2}
	if got := TaskDigest(replicatedTask, &svc, nil); got.Name != "api.2" {
		t.Errorf("name = %q, want api.2", got.Name)
	}

	global := svc
	global.Spec.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}

	globalTask := swarm.Task{ID: "t2", ServiceID: svc.ID, NodeID: "n1"}
	if got := TaskDigest(globalTask, &global, nil); got.Name != "api.n1" {
		t.Errorf("name = %q, want api.n1", got.Name)
	}
}

// An unassigned global task has no node yet; naming it "<service>." with a
// trailing dot would read as a truncated name rather than "not scheduled".
func TestTaskDigestGlobalTaskWithoutNodeFallsBackToServiceName(t *testing.T) {
	svc := replicated("api", 1)
	svc.Spec.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}

	task := swarm.Task{ID: "t1", ServiceID: svc.ID}

	got := TaskDigest(task, &svc, nil)

	if got.Name != "api" {
		t.Errorf("name = %q, want the service name with no trailing dot", got.Name)
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

// A stack's membership spans five resource types, and Related exists so a
// caller can traverse without a second search — listing only services would
// leave a caller four searches short of knowing what the stack owns.
func TestStackDigestRelatedIncludesAllMemberTypes(t *testing.T) {
	stack := cache.StackDetail{
		Name:     "demo",
		Services: []swarm.Service{replicated("api", 1)},
		Configs: []swarm.Config{
			{ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "conf"}}},
		},
		Secrets: []swarm.Secret{
			{ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}}},
		},
		Networks: []network.Summary{overlay("net1", "demo_overlay")},
		Volumes:  []volume.Volume{{Name: "data", Driver: "local"}},
	}

	got := StackDigest(stack, nil)

	if len(got.Related) != 5 {
		t.Fatalf("related = %d, want 5 (one per member type)", len(got.Related))
	}

	counts := map[string]int{}
	for _, r := range got.Related {
		if r.Relation != "contains" {
			t.Errorf("related[%s].relation = %q, want contains", r.Type, r.Relation)
		}
		counts[r.Type]++
	}

	for _, want := range []string{"service", "config", "secret", "network", "volume"} {
		if counts[want] != 1 {
			t.Errorf("related type %q count = %d, want 1", want, counts[want])
		}
	}

	for key, want := range map[string]any{
		"services": 1, "configs": 1, "secrets": 1, "networks": 1, "volumes": 1,
	} {
		if got.Details[key] != want {
			t.Errorf("details[%s] = %v, want %v", key, got.Details[key], want)
		}
	}
}

// A stack has no timestamp of its own; the newest member service's UpdatedAt
// is the only honest answer for when its composition last changed.
func TestStackDigestSinceIsNewestMemberUpdate(t *testing.T) {
	older := replicated("old", 1)
	older.UpdatedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	newer := replicated("new", 1)
	newer.UpdatedAt = time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)

	stack := cache.StackDetail{Name: "demo", Services: []swarm.Service{older, newer}}

	got := StackDigest(stack, nil)

	want := newer.UpdatedAt.UTC().Format(time.RFC3339)
	if got.Since != want {
		t.Errorf("since = %q, want %q (the newest member's UpdatedAt)", got.Since, want)
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

// Docker's volume CreatedAt is a free-form string, not guaranteed to be
// RFC3339. A value that does not parse must leave Since omitted rather than
// surface a bogus or half-parsed timestamp.
func TestVolumeDigestOmitsSinceWhenCreatedAtDoesNotParse(t *testing.T) {
	vol := volume.Volume{Name: "data", Driver: "local", CreatedAt: "not-a-timestamp"}

	got := VolumeDigest(vol, nil)

	if got.Since != "" {
		t.Errorf("since = %q, want empty for an unparseable CreatedAt", got.Since)
	}
}

// A CreatedAt that does parse as RFC3339 must be passed through.
func TestVolumeDigestReportsSinceWhenCreatedAtParses(t *testing.T) {
	vol := volume.Volume{Name: "data", Driver: "local", CreatedAt: "2026-09-04T12:00:00Z"}

	got := VolumeDigest(vol, nil)

	if got.Since != "2026-09-04T12:00:00Z" {
		t.Errorf("since = %q, want the parsed timestamp", got.Since)
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
