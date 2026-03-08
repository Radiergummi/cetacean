package filter

import (
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"cetacean/internal/cache"
)

func TestCompile_Valid(t *testing.T) {
	prog, err := Compile(`name == "test"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prog == nil {
		t.Fatal("expected non-nil program")
	}
}

func TestCompile_Invalid(t *testing.T) {
	_, err := Compile(`name ===`)
	if err == nil {
		t.Fatal("expected error for invalid expression")
	}
}

func TestEvaluate_True(t *testing.T) {
	prog, _ := Compile(`name == "web"`)
	ok, err := Evaluate(prog, map[string]any{"name": "web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true")
	}
}

func TestEvaluate_False(t *testing.T) {
	prog, _ := Compile(`name == "web"`)
	ok, err := Evaluate(prog, map[string]any{"name": "api"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected false")
	}
}

func TestEvaluate_Contains(t *testing.T) {
	prog, _ := Compile(`image contains "nginx"`)
	ok, err := Evaluate(prog, map[string]any{"image": "nginx:1.25-alpine"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for contains")
	}
}

func TestEvaluate_NotEqual(t *testing.T) {
	prog, _ := Compile(`state != "running"`)
	ok, err := Evaluate(prog, map[string]any{"state": "failed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for !=")
	}
}

func TestEvaluate_And(t *testing.T) {
	prog, _ := Compile(`state == "failed" && exit_code != "0"`)
	ok, err := Evaluate(prog, map[string]any{"state": "failed", "exit_code": "1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for AND")
	}
}

func TestEvaluate_Or(t *testing.T) {
	prog, _ := Compile(`state == "failed" || state == "rejected"`)
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
	env := NodeEnv(n)

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
	env := ServiceEnv(s)

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
	env := ServiceEnv(s)
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
	env := TaskEnv(task)

	if env["state"] != "failed" {
		t.Errorf("state=%v", env["state"])
	}
	if env["desired_state"] != "running" {
		t.Errorf("desired_state=%v", env["desired_state"])
	}
	if env["exit_code"] != "1" {
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
	env := NetworkEnv(n)

	if env["driver"] != "overlay" {
		t.Errorf("driver=%v", env["driver"])
	}
	if env["scope"] != "swarm" {
		t.Errorf("scope=%v", env["scope"])
	}
}

func TestVolumeEnv(t *testing.T) {
	v := volume.Volume{Name: "data", Driver: "local", Scope: "local"}
	env := VolumeEnv(v)

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
	env := StackEnv(s)

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

func TestResourceEnv_Dispatch(t *testing.T) {
	tests := []struct {
		name     string
		resource any
		wantNil  bool
	}{
		{"node", swarm.Node{ID: "n1"}, false},
		{"service", swarm.Service{ID: "s1"}, false},
		{"task", swarm.Task{ID: "t1"}, false},
		{"config", swarm.Config{ID: "c1"}, false},
		{"secret", swarm.Secret{ID: "s1"}, false},
		{"network", network.Summary{ID: "n1"}, false},
		{"volume", volume.Volume{Name: "v1"}, false},
		{"nil", nil, true},
		{"unknown", "string", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := ResourceEnv(tt.resource)
			if tt.wantNil && env != nil {
				t.Errorf("expected nil env for %s", tt.name)
			}
			if !tt.wantNil && env == nil {
				t.Errorf("expected non-nil env for %s", tt.name)
			}
		})
	}
}

func TestConfigEnv(t *testing.T) {
	c := swarm.Config{ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app-config"}}}
	env := ConfigEnv(c)
	if env["id"] != "c1" {
		t.Errorf("id=%v", env["id"])
	}
	if env["name"] != "app-config" {
		t.Errorf("name=%v", env["name"])
	}
}

func TestSecretEnv(t *testing.T) {
	s := swarm.Secret{ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "tls-cert"}}}
	env := SecretEnv(s)
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
	env := TaskEnv(task)
	if env["image"] != "" {
		t.Errorf("image=%v, want empty", env["image"])
	}
	if env["exit_code"] != "" {
		t.Errorf("exit_code=%v, want empty", env["exit_code"])
	}
}

func TestServiceEnv_NilContainerSpec(t *testing.T) {
	s := swarm.Service{
		ID:   "s1",
		Spec: swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{ContainerSpec: nil}},
	}
	env := ServiceEnv(s)
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
	env := ServiceEnv(s)
	if env["stack"] != "" {
		t.Errorf("stack=%v, want empty", env["stack"])
	}
}

// --- Expression operator coverage ---

func TestEvaluate_StartsWith(t *testing.T) {
	prog, _ := Compile(`name startsWith "web-"`)
	ok, err := Evaluate(prog, map[string]any{"name": "web-frontend"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for startsWith")
	}
}

func TestEvaluate_EndsWith(t *testing.T) {
	prog, _ := Compile(`name endsWith "-prod"`)
	ok, err := Evaluate(prog, map[string]any{"name": "api-prod"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for endsWith")
	}
}

func TestEvaluate_NumericComparison(t *testing.T) {
	prog, _ := Compile(`services > 2`)
	ok, err := Evaluate(prog, map[string]any{"services": 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for services > 2")
	}
}

func TestEvaluate_MissingEnvKey(t *testing.T) {
	prog, _ := Compile(`role == "manager"`)
	// env has no "role" key — expr treats missing keys as nil
	ok, err := Evaluate(prog, map[string]any{"name": "web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// nil != "manager", so should be false
	if ok {
		t.Error("expected false for missing env key")
	}
}

func TestEvaluate_Matches(t *testing.T) {
	prog, _ := Compile(`name matches "^web-[0-9]+$"`)
	ok, err := Evaluate(prog, map[string]any{"name": "web-42"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for regex match")
	}
}

func TestEvaluate_Matches_NoMatch(t *testing.T) {
	prog, _ := Compile(`name matches "^web-[0-9]+$"`)
	ok, err := Evaluate(prog, map[string]any{"name": "api-server"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected false for non-matching regex")
	}
}

func TestEvaluate_Not(t *testing.T) {
	prog, _ := Compile(`!(state == "running")`)
	ok, err := Evaluate(prog, map[string]any{"state": "failed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for negation")
	}
}

func TestEvaluate_In(t *testing.T) {
	prog, _ := Compile(`state in ["failed", "rejected", "orphaned"]`)
	ok, err := Evaluate(prog, map[string]any{"state": "rejected"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for in-list")
	}
}

func TestEvaluate_In_NoMatch(t *testing.T) {
	prog, _ := Compile(`state in ["failed", "rejected"]`)
	ok, err := Evaluate(prog, map[string]any{"state": "running"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected false for not-in-list")
	}
}
