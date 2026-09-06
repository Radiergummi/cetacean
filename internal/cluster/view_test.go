package cluster

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
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

	rows := RowsForServices([]swarm.Service{svc}, map[string]int{"svc-api": 2})

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
			[]EnrichedTask{{
				Task: swarm.Task{ID: "t1", ServiceID: "svc-api", NodeID: "n1",
					Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
				NodeHostname: "worker-a",
			}},
			[]swarm.Service{replicated("api", 1)},
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

	got := ServiceDigest(svc, tasks, nil, nil)

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

	got := ServiceDigest(svc, tasks, nil, nil)

	if got.Reason != "" {
		t.Errorf("reason = %q, want empty for a converged service", got.Reason)
	}
}

// A nil Go slice and an empty one both report len() == 0, so the Go-value
// assertions above never notice recentFailures marshalling to null. Only the
// encoding proves the output schema's slices are always arrays.
func TestServiceDigestMarshalsEmptySlicesNotNull(t *testing.T) {
	svc := replicated("api", 1)
	tasks := []swarm.Task{{
		ID: "t1", ServiceID: "svc-api", DesiredState: swarm.TaskStateRunning,
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
	}}

	got := ServiceDigest(svc, tasks, nil, nil)

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if !strings.Contains(string(data), `"recentFailures":[]`) {
		t.Errorf("json = %s, want recentFailures as an empty array, not null", data)
	}
	if !strings.Contains(string(data), `"related":[]`) {
		t.Errorf("json = %s, want related as an empty array, not null", data)
	}
}

// Ordinary startup churn is not a failure. A task still coming up during a
// scale-up sits in "preparing" with no error, and DeriveServiceState already
// reports "pending" for the replica shortfall — recentFailures must not
// double that up as though something were broken.
func TestServiceDigestIgnoresNormalStartup(t *testing.T) {
	svc := replicated("scaling", 3)

	tasks := []swarm.Task{
		{ID: "t1", ServiceID: "svc-scaling", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
		{ID: "t2", ServiceID: "svc-scaling", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStatePreparing, Message: "preparing"}},
	}

	got := ServiceDigest(svc, tasks, nil, nil)

	if len(got.RecentFailures) != 0 {
		t.Errorf(
			"recentFailures = %d, want none for a task still starting",
			len(got.RecentFailures),
		)
	}
	if got.Reason != "" {
		t.Errorf("reason = %q, want empty: starting up is not a failure", got.Reason)
	}
}

// Since dates the problem back to its first occurrence, not its most recent
// one — recentFailures[0] answers "what does it look like now", since
// answers "how long has it looked like this".
func TestServiceDigestSinceIsTheOldestFailure(t *testing.T) {
	svc := replicated("stuck", 2)

	older := time.Date(2026, 9, 4, 16, 15, 11, 0, time.UTC)
	newer := time.Date(2026, 9, 4, 17, 44, 8, 0, time.UTC)

	tasks := []swarm.Task{
		{ID: "t1", ServiceID: "svc-stuck", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{
				State: swarm.TaskStateRejected, Timestamp: older, Err: "first failure",
			}},
		{ID: "t2", ServiceID: "svc-stuck", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{
				State: swarm.TaskStateRejected, Timestamp: newer, Err: "second failure",
			}},
	}

	got := ServiceDigest(svc, tasks, nil, nil)

	want := older.UTC().Format(time.RFC3339)
	if got.Since != want {
		t.Errorf("since = %q, want %q (the older failure, not the newer one)", got.Since, want)
	}
}

// A rolling update with no failing task still needs an explanation:
// DeriveServiceState reports "updating" from UpdateStatus alone, and without
// this fallback the digest would report a non-healthy state with nothing
// behind it.
func TestServiceDigestReasonFallsBackToUpdateStatus(t *testing.T) {
	svc := replicated("api", 1)
	svc.UpdateStatus = &swarm.UpdateStatus{
		State:   swarm.UpdateStateUpdating,
		Message: "update in progress (1 out of 1)",
	}

	tasks := []swarm.Task{{
		ID: "t1", ServiceID: "svc-api", DesiredState: swarm.TaskStateRunning,
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
	}}

	got := ServiceDigest(svc, tasks, nil, nil)

	if got.State != "updating" {
		t.Fatalf("state = %q, want updating", got.State)
	}
	if got.Reason != "update in progress (1 out of 1)" {
		t.Errorf("reason = %q, want the update status message", got.Reason)
	}
}

// Two failures in the same second must still sort deterministically: At
// alone cannot break the tie, and SortFunc is not stable.
func TestServiceDigestFailuresTieBreakOnTaskID(t *testing.T) {
	svc := replicated("stuck", 2)

	same := time.Date(2026, 9, 4, 17, 44, 8, 0, time.UTC)

	tasks := []swarm.Task{
		{ID: "t2", ServiceID: "svc-stuck", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRejected, Timestamp: same, Err: "b"}},
		{ID: "t1", ServiceID: "svc-stuck", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRejected, Timestamp: same, Err: "a"}},
	}

	got := ServiceDigest(svc, tasks, nil, nil)

	if len(got.RecentFailures) != 2 || got.RecentFailures[0].TaskID != "t1" {
		t.Errorf("order = %+v, want t1 before t2 on a timestamp tie", got.RecentFailures)
	}
}

// Units are named because the alternative shipped a bug: a recommendation once
// reported "configured 25, suggested 50000000" for the same quantity. A field
// name that carries its unit cannot be misread.
func TestServiceDetailsNameTheirUnits(t *testing.T) {
	svc := replicated("api", 2)
	svc.Spec.TaskTemplate.Resources = &swarm.ResourceRequirements{
		Limits:       &swarm.Limit{NanoCPUs: 2e9, MemoryBytes: 1 << 30},
		Reservations: &swarm.Resources{NanoCPUs: 5e8, MemoryBytes: 256 << 20},
	}
	svc.Spec.TaskTemplate.ContainerSpec.Healthcheck = &container.HealthConfig{
		Test:     []string{"CMD", "true"},
		Interval: 10 * time.Second,
	}
	svc.Spec.TaskTemplate.ContainerSpec.Env = []string{"DB_PASSWORD=hunter2", "PORT=8080"}

	got := ServiceDetails(svc)

	if got["cpuLimitCores"] != 2.0 {
		t.Errorf("cpuLimitCores = %v, want 2", got["cpuLimitCores"])
	}
	if got["memoryLimitBytes"] != int64(1<<30) {
		t.Errorf("memoryLimitBytes = %v, want 1073741824", got["memoryLimitBytes"])
	}
	if got["healthcheckInterval"] != "10s" {
		t.Errorf("healthcheckInterval = %v, want \"10s\"", got["healthcheckInterval"])
	}

	// Env names only: env is where credentials live.
	names, ok := got["envNames"].([]string)
	if !ok {
		t.Fatalf("envNames = %T, want []string", got["envNames"])
	}
	if !slices.Equal(names, []string{"DB_PASSWORD", "PORT"}) {
		t.Errorf("envNames = %v, want the names sorted", names)
	}

	for key, value := range got {
		if str, isStr := value.(string); isStr && strings.Contains(str, "hunter2") {
			t.Fatalf("details[%q] leaked an env value", key)
		}
	}
}

// A network attachment carries only an ID; the related entry must resolve it
// to the name a caller can act on, following the ID-and-Name-always-present
// rule Row and Digest both hold to.
func TestServiceDigestRelatedResolvesNetworkName(t *testing.T) {
	svc := replicated("api", 1)
	svc.Spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{
		{Target: "net1"},
	}

	tasks := []swarm.Task{{
		ID: "t1", ServiceID: "svc-api", DesiredState: swarm.TaskStateRunning,
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
	}}

	got := ServiceDigest(svc, tasks, []network.Summary{overlay("net1", "demo_overlay")}, nil)

	if len(got.Related) != 1 {
		t.Fatalf("related = %d, want 1", len(got.Related))
	}
	if got.Related[0].Name != "demo_overlay" {
		t.Errorf("related[0].name = %q, want the network's name", got.Related[0].Name)
	}
	if got.Related[0].ID != "net1" {
		t.Errorf("related[0].id = %q, want net1", got.Related[0].ID)
	}
	if got.Related[0].Type != "network" || got.Related[0].Relation != "attached-to" {
		t.Errorf("related[0] = %+v, want type network / relation attached-to", got.Related[0])
	}
}

// A caller may have been handed a networks slice already filtered by ACL, in
// which case falling back to the ID beats emitting an entry with an empty
// name.
func TestServiceDigestRelatedFallsBackToIDWhenNetworkUnknown(t *testing.T) {
	svc := replicated("api", 1)
	svc.Spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{
		{Target: "net1"},
	}

	tasks := []swarm.Task{{
		ID: "t1", ServiceID: "svc-api", DesiredState: swarm.TaskStateRunning,
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
	}}

	got := ServiceDigest(svc, tasks, nil, nil)

	if len(got.Related) != 1 {
		t.Fatalf("related = %d, want 1", len(got.Related))
	}
	if got.Related[0].Name != "net1" {
		t.Errorf("related[0].name = %q, want the ID as a fallback", got.Related[0].Name)
	}
}

// A bind mount names a host path, not a Cetacean resource — there is nothing
// to traverse to, so only the volume mount earns a related entry.
func TestServiceDigestRelatedOnlyIncludesVolumeMounts(t *testing.T) {
	svc := replicated("api", 1)
	svc.Spec.TaskTemplate.ContainerSpec.Mounts = []mount.Mount{
		{Type: mount.TypeVolume, Source: "data", Target: "/data"},
		{Type: mount.TypeBind, Source: "/host/path", Target: "/container/path"},
	}

	tasks := []swarm.Task{{
		ID: "t1", ServiceID: "svc-api", DesiredState: swarm.TaskStateRunning,
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
	}}

	got := ServiceDigest(svc, tasks, nil, nil)

	if len(got.Related) != 1 {
		t.Fatalf("related = %d, want 1 (the volume, not the bind mount)", len(got.Related))
	}
	if got.Related[0].Type != "volume" || got.Related[0].Relation != "mounts" {
		t.Errorf("related[0] = %+v, want type volume / relation mounts", got.Related[0])
	}
	if got.Related[0].ID != "data" || got.Related[0].Name != "data" {
		t.Errorf(
			"related[0] id/name = %q/%q, want both to be the volume name",
			got.Related[0].ID,
			got.Related[0].Name,
		)
	}
}

// Ports are the spec's own CamelCase field names read verbatim once already;
// the whole point of this representation is that a caller never has to know
// that PublishedPort means "published".
func TestServiceDetailsNamesPortFields(t *testing.T) {
	svc := replicated("api", 1)
	svc.Spec.EndpointSpec = &swarm.EndpointSpec{
		Ports: []swarm.PortConfig{
			{
				PublishedPort: 8080,
				TargetPort:    80,
				Protocol:      swarm.PortConfigProtocolTCP,
				PublishMode:   swarm.PortConfigPublishModeIngress,
			},
		},
	}

	got := ServiceDetails(svc)

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(data), "PublishedPort") {
		t.Fatalf("json = %s, want no raw Docker field names", data)
	}

	ports, ok := got["ports"].([]map[string]any)
	if !ok {
		t.Fatalf("ports = %T, want []map[string]any", got["ports"])
	}
	if len(ports) != 1 {
		t.Fatalf("ports = %d, want 1", len(ports))
	}

	port := ports[0]
	if port["published"] != uint32(8080) {
		t.Errorf("published = %v, want 8080", port["published"])
	}
	if port["target"] != uint32(80) {
		t.Errorf("target = %v, want 80", port["target"])
	}
	if port["protocol"] != string(swarm.PortConfigProtocolTCP) {
		t.Errorf("protocol = %v, want tcp", port["protocol"])
	}
	if port["mode"] != string(swarm.PortConfigPublishModeIngress) {
		t.Errorf("mode = %v, want ingress", port["mode"])
	}
}

// Delay and Monitor are time.Duration, i.e. unlabelled nanosecond integers in
// Docker's own type. The global constraint is explicit: every numeric field
// names its unit or is a duration string.
func TestServiceDetailsUpdatePolicyUsesDurationStrings(t *testing.T) {
	svc := replicated("api", 1)
	svc.Spec.UpdateConfig = &swarm.UpdateConfig{
		Parallelism: 2,
		Delay:       10 * time.Second,
	}

	got := ServiceDetails(svc)

	policy, ok := got["updatePolicy"].(map[string]any)
	if !ok {
		t.Fatalf("updatePolicy = %T, want map[string]any", got["updatePolicy"])
	}
	if policy["parallelism"] != uint64(2) {
		t.Errorf("parallelism = %v, want 2", policy["parallelism"])
	}
	if policy["delay"] != "10s" {
		t.Errorf("delay = %v, want \"10s\", not a nanosecond count", policy["delay"])
	}
}

// A service with none of these configured must omit the keys entirely rather
// than emit empty maps a caller has to check the length of.
func TestServiceDetailsOmitsAbsentPolicyAndPorts(t *testing.T) {
	svc := replicated("api", 1)

	got := ServiceDetails(svc)

	if _, ok := got["ports"]; ok {
		t.Errorf("ports present = %v, want omitted", got["ports"])
	}
	if _, ok := got["updatePolicy"]; ok {
		t.Errorf("updatePolicy present = %v, want omitted", got["updatePolicy"])
	}
	if _, ok := got["rollbackPolicy"]; ok {
		t.Errorf("rollbackPolicy present = %v, want omitted", got["rollbackPolicy"])
	}
}

// A crash loop is made entirely of tasks the orchestrator has already
// replaced: Swarm marks a task for shutdown the instant it fails and starts
// another, so by the time anything reads the digest every failure it has is a
// replaced one. Excluding them left "why is this restarting in a loop"
// answered with an empty list — the one question the field exists for.
//
// The exclusion was written for a different case, a service that failed once
// and has since recovered, and it only ever appeared to work because the cache
// held those tasks with a stale DesiredState that let them through. Once the
// watcher learned to observe the terminal transition, the guard did what it
// said and the failures vanished. What separates the two cases is the task's
// state, not the orchestrator's intent for it: a shutdown that was clean is
// not a failure and is excluded a few lines down regardless.
func TestServiceDigestReportsFailuresSwarmHasAlreadyReplaced(t *testing.T) {
	svc := replicated("flaky", 1)

	tasks := []swarm.Task{
		{ID: "t-old", ServiceID: "svc-flaky", DesiredState: swarm.TaskStateShutdown,
			Status: swarm.TaskStatus{
				State: swarm.TaskStateFailed,
				Err:   "task: non-zero exit (1)",
			}},
		{ID: "t-new", ServiceID: "svc-flaky", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
	}

	got := ServiceDigest(svc, tasks, nil, nil)

	if len(got.RecentFailures) != 1 {
		t.Fatalf(
			"recentFailures = %d, want the replaced task that failed",
			len(got.RecentFailures),
		)
	}

	if got.RecentFailures[0].TaskID != "t-old" {
		t.Errorf("recentFailures[0] = %q, want t-old", got.RecentFailures[0].TaskID)
	}
}

// A replaced task that stopped cleanly is history and stays out: a rolling
// update leaves one behind for every replica it moved, and reporting those as
// failures would make every successful deploy look like an incident.
func TestServiceDigestIgnoresCleanlyReplacedTasks(t *testing.T) {
	svc := replicated("rolled", 1)

	tasks := []swarm.Task{
		{ID: "t-old", ServiceID: "svc-rolled", DesiredState: swarm.TaskStateShutdown,
			Status: swarm.TaskStatus{State: swarm.TaskStateShutdown}},
		{ID: "t-new", ServiceID: "svc-rolled", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
	}

	if got := ServiceDigest(svc, tasks, nil, nil); len(got.RecentFailures) != 0 {
		t.Errorf(
			"recentFailures = %d, want none for a cleanly replaced task",
			len(got.RecentFailures),
		)
	}
}
