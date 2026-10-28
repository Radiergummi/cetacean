package filter

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestCompile_Valid(t *testing.T) {
	prog, err := Compile(`name == "test"`, Services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prog == nil {
		t.Fatal("expected non-nil program")
	}
}

func TestCompile_Invalid(t *testing.T) {
	_, err := Compile(`name ===`, Services)
	if err == nil {
		t.Fatal("expected error for invalid expression")
	}
}

func TestEvaluate_True(t *testing.T) {
	prog, _ := Compile(`name == "web"`, Services)
	ok, err := Evaluate(prog, map[string]any{"name": "web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true")
	}
}

func TestEvaluate_False(t *testing.T) {
	prog, _ := Compile(`name == "web"`, Services)
	ok, err := Evaluate(prog, map[string]any{"name": "api"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected false")
	}
}

func TestEvaluate_Contains(t *testing.T) {
	prog, _ := Compile(`image contains "nginx"`, Services)
	ok, err := Evaluate(prog, map[string]any{"image": "nginx:1.25-alpine"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for contains")
	}
}

func TestEvaluate_NotEqual(t *testing.T) {
	prog, _ := Compile(`state != "running"`, Tasks)
	ok, err := Evaluate(prog, map[string]any{"state": "failed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for !=")
	}
}

func TestEvaluate_And(t *testing.T) {
	prog, _ := Compile(`state == "failed" && exit_code != 0`, Tasks)
	ok, err := Evaluate(prog, map[string]any{"state": "failed", "exit_code": 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for AND")
	}
}

func TestEvaluate_Or(t *testing.T) {
	prog, _ := Compile(`state == "failed" || state == "rejected"`, Tasks)
	ok, err := Evaluate(prog, map[string]any{"state": "rejected"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for OR")
	}
}

func TestNodeEnv(t *testing.T) {
	n := swarm.Node{ID: "n1", Spec: swarm.NodeSpec{
		Role:         swarm.NodeRoleManager,
		Availability: swarm.NodeAvailabilityActive,
	}, Status: swarm.NodeStatus{State: swarm.NodeStateReady}}
	n.Description.Hostname = "manager-01"
	env := NodeEnv(n, nil)

	if env["id"] != "n1" {
		t.Errorf("id=%v", env["id"])
	}
	if env["name"] != "manager-01" {
		t.Errorf("name=%v", env["name"])
	}
	if env["state"] != "ready" {
		t.Errorf("state=%v", env["state"])
	}
	if env["role"] != "manager" {
		t.Errorf("role=%v", env["role"])
	}
	if env["availability"] != "active" {
		t.Errorf("availability=%v", env["availability"])
	}
}

func TestServiceEnv(t *testing.T) {
	s := swarm.Service{
		ID: "s1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "web",
				Labels: map[string]string{"com.docker.stack.namespace": "mystack"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:latest"},
			},
			Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}},
		},
	}
	env := ServiceEnv(s, nil)

	if env["name"] != "web" {
		t.Errorf("name=%v", env["name"])
	}
	if env["image"] != "nginx:latest" {
		t.Errorf("image=%v", env["image"])
	}
	if env["mode"] != "global" {
		t.Errorf("mode=%v", env["mode"])
	}
	if env["stack"] != "mystack" {
		t.Errorf("stack=%v", env["stack"])
	}
}

func TestServiceEnv_Replicated(t *testing.T) {
	s := swarm.Service{Spec: swarm.ServiceSpec{
		Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{}},
	}}
	env := ServiceEnv(s, nil)
	if env["mode"] != "replicated" {
		t.Errorf("mode=%v", env["mode"])
	}
}

func TestTaskEnv(t *testing.T) {
	task := swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		NodeID:    "node1",
		Slot:      3,
		Spec: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "app:v2"},
		},
		Status: swarm.TaskStatus{
			State:           swarm.TaskStateFailed,
			Err:             "exit code 1",
			ContainerStatus: &swarm.ContainerStatus{ExitCode: 1},
		},
		DesiredState: swarm.TaskStateRunning,
	}
	env := TaskEnv(task, nil)

	if env["state"] != "failed" {
		t.Errorf("state=%v", env["state"])
	}
	if env["desired_state"] != "running" {
		t.Errorf("desired_state=%v", env["desired_state"])
	}
	if env["exit_code"] != 1 {
		t.Errorf("exit_code=%v", env["exit_code"])
	}
	if env["error"] != "exit code 1" {
		t.Errorf("error=%v", env["error"])
	}
	if env["service"] != "svc1" {
		t.Errorf("service=%v", env["service"])
	}
	if env["node"] != "node1" {
		t.Errorf("node=%v", env["node"])
	}
	if env["slot"] != 3 {
		t.Errorf("slot=%v", env["slot"])
	}
}

func TestNetworkEnv(t *testing.T) {
	n := network.Summary{ID: "n1", Name: "overlay_net", Driver: "overlay", Scope: "swarm"}
	env := NetworkEnv(n, nil)

	if env["driver"] != "overlay" {
		t.Errorf("driver=%v", env["driver"])
	}
	if env["scope"] != "swarm" {
		t.Errorf("scope=%v", env["scope"])
	}
}

func TestVolumeEnv(t *testing.T) {
	v := volume.Volume{Name: "data", Driver: "local", Scope: "local"}
	env := VolumeEnv(v, nil)

	if env["name"] != "data" {
		t.Errorf("name=%v", env["name"])
	}
	if env["driver"] != "local" {
		t.Errorf("driver=%v", env["driver"])
	}
}

func TestStackEnv(t *testing.T) {
	s := cache.Stack{
		Name:     "mystack",
		Services: []string{"s1", "s2"},
		Configs:  []string{"c1"},
	}
	env := StackEnv(s, nil)

	if env["name"] != "mystack" {
		t.Errorf("name=%v", env["name"])
	}
	if env["services"] != 2 {
		t.Errorf("services=%v", env["services"])
	}
	if env["configs"] != 1 {
		t.Errorf("configs=%v", env["configs"])
	}
}

func TestConfigEnv(t *testing.T) {
	c := swarm.Config{
		ID:   "c1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app-config"}},
	}
	env := ConfigEnv(c, nil)
	if env["id"] != "c1" {
		t.Errorf("id=%v", env["id"])
	}
	if env["name"] != "app-config" {
		t.Errorf("name=%v", env["name"])
	}
}

func TestSecretEnv(t *testing.T) {
	s := swarm.Secret{
		ID:   "s1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "tls-cert"}},
	}
	env := SecretEnv(s, nil)
	if env["id"] != "s1" {
		t.Errorf("id=%v", env["id"])
	}
	if env["name"] != "tls-cert" {
		t.Errorf("name=%v", env["name"])
	}
}

// --- Nil/zero-value edge cases ---

func TestTaskEnv_NilContainerSpec(t *testing.T) {
	task := swarm.Task{
		ID:     "t1",
		Spec:   swarm.TaskSpec{ContainerSpec: nil},
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning, ContainerStatus: nil},
	}
	env := TaskEnv(task, nil)
	if env["image"] != "" {
		t.Errorf("image=%v, want empty", env["image"])
	}
	if env["exit_code"] != 0 {
		t.Errorf("exit_code=%v, want 0", env["exit_code"])
	}
}

func TestServiceEnv_NilContainerSpec(t *testing.T) {
	s := swarm.Service{
		ID:   "s1",
		Spec: swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{ContainerSpec: nil}},
	}
	env := ServiceEnv(s, nil)
	if env["image"] != "" {
		t.Errorf("image=%v, want empty", env["image"])
	}
}

func TestServiceEnv_NoStackLabel(t *testing.T) {
	s := swarm.Service{
		ID: "s1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web", Labels: map[string]string{"other": "val"}},
		},
	}
	env := ServiceEnv(s, nil)
	if env["stack"] != "" {
		t.Errorf("stack=%v, want empty", env["stack"])
	}
}

// --- Expression operator coverage ---

func TestEvaluate_StartsWith(t *testing.T) {
	prog, _ := Compile(`name startsWith "web-"`, Services)
	ok, err := Evaluate(prog, map[string]any{"name": "web-frontend"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for startsWith")
	}
}

func TestEvaluate_EndsWith(t *testing.T) {
	prog, _ := Compile(`name endsWith "-prod"`, Services)
	ok, err := Evaluate(prog, map[string]any{"name": "api-prod"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for endsWith")
	}
}

func TestEvaluate_NumericComparison(t *testing.T) {
	prog, _ := Compile(`services > 2`, Stacks)
	ok, err := Evaluate(prog, map[string]any{"services": 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for services > 2")
	}
}

// The field types come from a zero item, so a populated one must set no other.
func TestBuildSetsTheFieldsOfAZeroItem(t *testing.T) {
	task := swarm.Task{
		Spec: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "app:v2"}},
		Status: swarm.TaskStatus{
			State:           swarm.TaskStateFailed,
			ContainerStatus: &swarm.ContainerStatus{ExitCode: 1},
		},
	}
	service := swarm.Service{Spec: swarm.ServiceSpec{
		Mode:         swarm.ServiceMode{Global: &swarm.GlobalService{}},
		TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "app:v2"}},
	}}
	for name, pair := range map[string][2]map[string]any{
		"task":    {Tasks.Build(task, nil), Tasks.types},
		"service": {Services.Build(service, nil), Services.types},
	} {
		if len(pair[0])+1 != len(pair[1]) {
			t.Errorf(
				"%s: populated item sets %d fields, zero item %d",
				name,
				len(pair[0]),
				len(pair[1])-1,
			)
		}
	}
}

func TestEvaluate_Matches(t *testing.T) {
	prog, _ := Compile(`name matches "^web-[0-9]+$"`, Services)
	ok, err := Evaluate(prog, map[string]any{"name": "web-42"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for regex match")
	}
}

func TestEvaluate_Matches_NoMatch(t *testing.T) {
	prog, _ := Compile(`name matches "^web-[0-9]+$"`, Services)
	ok, err := Evaluate(prog, map[string]any{"name": "api-server"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected false for non-matching regex")
	}
}

func TestEvaluate_Not(t *testing.T) {
	prog, _ := Compile(`!(state == "running")`, Tasks)
	ok, err := Evaluate(prog, map[string]any{"state": "failed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for negation")
	}
}

func TestEvaluate_In(t *testing.T) {
	prog, _ := Compile(`state in ["failed", "rejected", "orphaned"]`, Tasks)
	ok, err := Evaluate(prog, map[string]any{"state": "rejected"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for in-list")
	}
}

func TestEvaluate_In_NoMatch(t *testing.T) {
	prog, _ := Compile(`state in ["failed", "rejected"]`, Tasks)
	ok, err := Evaluate(prog, map[string]any{"state": "running"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected false for not-in-list")
	}
}

func TestCompile_UnknownField(t *testing.T) {
	_, err := Compile(`staet == "failed"`, Tasks)
	if err == nil || !strings.Contains(err.Error(), "staet") {
		t.Fatalf("err = %v, want one naming staet", err)
	}
}

func TestCompile_MismatchedType(t *testing.T) {
	for _, expression := range []string{`exit_code != "0"`, `name > 1`, `name`} {
		if _, err := Compile(expression, Tasks); err == nil {
			t.Errorf("%s compiled against tasks", expression)
		}
	}
}

// The cache holds typed programs, so one expression must answer per type.
func TestCompile_CachesPerType(t *testing.T) {
	if _, err := Compile(`slot == 1`, Tasks); err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if _, err := Compile(`slot == 1`, Services); err == nil {
		t.Error("slot compiled against services after compiling against tasks")
	}
	if _, err := Compile(`slot == 1`, Tasks); err != nil {
		t.Errorf("tasks after services: %v", err)
	}
}

func TestExitCodeOrdersAcrossStates(t *testing.T) {
	prog, err := Compile(`exit_code > 0`, Tasks)
	if err != nil {
		t.Fatal(err)
	}
	running := swarm.Task{Status: swarm.TaskStatus{
		State:           swarm.TaskStateRunning,
		ContainerStatus: &swarm.ContainerStatus{ExitCode: -1},
	}}
	failed := swarm.Task{Status: swarm.TaskStatus{
		State:           swarm.TaskStateFailed,
		ContainerStatus: &swarm.ContainerStatus{ExitCode: 137},
	}}
	for _, tc := range []struct {
		task swarm.Task
		want bool
	}{{running, false}, {failed, true}} {
		got, err := Evaluate(prog, Tasks.Build(tc.task, nil))
		if err != nil || got != tc.want {
			t.Errorf("%s: got %v, %v; want %v", tc.task.Status.State, got, err, tc.want)
		}
	}
}

func TestPortEnv(t *testing.T) {
	port := cache.PublishedPort{
		PublishedPort: 8080,
		TargetPort:    80,
		Protocol:      swarm.PortConfigProtocolTCP,
		PublishMode:   swarm.PortConfigPublishModeIngress,
		ServiceName:   "shop_web",
		Stack:         "shop",
	}

	for expression, want := range map[string]bool{
		`published == 8080`:                        true,
		`published > 1024 && protocol == "tcp"`:    true,
		`mode == "host"`:                           false,
		`service == "shop_web" && stack == "shop"`: true,
		`nodes == 0`:                               true,
	} {
		prog, err := Compile(expression, Ports)
		if err != nil {
			t.Fatalf("Compile(%q): %v", expression, err)
		}

		got, err := Evaluate(prog, PortEnv(port, nil))
		if err != nil {
			t.Fatalf("Evaluate(%q): %v", expression, err)
		}

		if got != want {
			t.Errorf("%s = %v, want %v", expression, got, want)
		}
	}

	if _, err := Compile(`published == "8080"`, Ports); err == nil {
		t.Error("comparing the port to a string should be refused")
	}
}
