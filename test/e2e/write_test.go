//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// TestScaleServiceTakesEffectOnTheCluster drives the scale endpoint against a
// real engine and asserts the change on the engine itself, not on the
// handler's response: a handler could answer 200 and touch nothing.
func TestScaleServiceTakesEffectOnTheCluster(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	stack := fixtures.DeployStack(t, env, "scale", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})

	proc := sut.Start(t, sut.Config{
		Port:       19001,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":        "none",
			"CETACEAN_OPERATIONS_LEVEL": "1",
		},
	})

	service := stack + "_app"
	// GET /services/{id} is a direct cache-map lookup keyed by the Docker
	// service ID, not a name resolver (see serviceID in api_test.go), so the
	// name the stack was deployed under must be resolved first.
	id := serviceID(t, proc, service)

	body, err := json.Marshal(map[string]any{"replicas": 3})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut,
		proc.BaseURL+"/services/"+id+"/scale", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT scale: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Assert against the engine, not the response: the point of this lane is
	// that the cluster genuinely changed. The Docker Engine API accepts a
	// service name here (unlike Cetacean's own ID-only cache lookup above).
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		svc, _, err := env.Docker.ServiceInspectWithRaw(
			t.Context(),
			service,
			swarm.ServiceInspectOptions{},
		)
		if err == nil && svc.Spec.Mode.Replicated != nil &&
			*svc.Spec.Mode.Replicated.Replicas == 3 {
			return
		}

		time.Sleep(2 * time.Second)
	}

	t.Errorf("service %s never reached 3 replicas on the engine", service)
}

// TestScaleRefusedAtReadOnlyLevel: at operations level 0 the write endpoints
// must refuse with OPS001 rather than pretending the route doesn't exist.
func TestScaleRefusedAtReadOnlyLevel(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	stack := fixtures.DeployStack(t, env, "opslevel", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})

	proc := sut.Start(t, sut.Config{
		Port:       19001,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":        "none",
			"CETACEAN_OPERATIONS_LEVEL": "0",
		},
	})

	id := serviceID(t, proc, stack+"_app")

	body, err := json.Marshal(map[string]any{"replicas": 2})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut,
		proc.BaseURL+"/services/"+id+"/scale", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT scale: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}

	var problem struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}

	// requireWriteACL runs outermost (see svcTier1 in router.go) but auth
	// mode none bypasses ACL entirely, so requireLevel's OPS001 is the one
	// that actually fires here.
	if !strings.Contains(problem.Type, "OPS001") {
		t.Errorf("problem type = %q, want it to name OPS001", problem.Type)
	}
}

// TestRestartServiceRecreatesTasksOnTheCluster is not required by the brief,
// but is cheap given the fixtures above and exercises a second write path
// (POST, force-update) with the same real-engine discipline: it asserts a
// new task actually appeared rather than trusting the 200.
func TestRestartServiceRecreatesTasksOnTheCluster(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	stack := fixtures.DeployStack(t, env, "restart", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})

	proc := sut.Start(t, sut.Config{
		Port:       19001,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":        "none",
			"CETACEAN_OPERATIONS_LEVEL": "1",
		},
	})

	service := stack + "_app"
	id := serviceID(t, proc, service)

	before := serviceTaskIDs(t, env, id)
	if len(before) == 0 {
		t.Fatalf("service %s has no tasks before restart", service)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		proc.BaseURL+"/services/"+id+"/restart", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("POST restart: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		after := serviceTaskIDs(t, env, id)
		for taskID := range after {
			if !before[taskID] {
				return // a task not present before restart showed up on the engine
			}
		}

		time.Sleep(2 * time.Second)
	}

	t.Errorf("service %s never got a new task on the engine after restart", service)
}

// serviceTaskIDs returns the IDs of every task the engine currently
// associates with serviceID.
func serviceTaskIDs(t *testing.T, env *harness.Env, serviceID string) map[string]bool {
	t.Helper()

	tasks, err := env.Docker.TaskList(t.Context(), swarm.TaskListOptions{})
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}

	ids := make(map[string]bool)
	for _, task := range tasks {
		if task.ServiceID == serviceID {
			ids[task.ID] = true
		}
	}

	return ids
}
