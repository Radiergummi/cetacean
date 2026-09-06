package cluster

import (
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
)

// TestServiceDetailsSeparatesCommandFromArgs fixes a mislabel that only starts
// to bite now that a caller can *write* this section.
//
// ServiceDetails reported spec.Args under the key "command" and never reported
// spec.Command at all. Docker draws the line the other way: Command is the
// entrypoint, Args is what follows it. A service with an entrypoint override
// therefore had it hidden, and — once update_service gains a "command" section
// — a caller would write Command and read back Args under the same name.
func TestServiceDetailsSeparatesCommandFromArgs(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Command: []string{"sh", "-c"},
					Args:    []string{"echo hi"},
				},
			},
		},
	}

	got := ServiceDetails(svc)

	command, ok := got["command"].([]string)
	if !ok {
		t.Fatalf("command = %v (%T), want the entrypoint slice", got["command"], got["command"])
	}
	if len(command) != 2 || command[0] != "sh" || command[1] != "-c" {
		t.Errorf("command = %v, want [sh -c] (the entrypoint, not the arguments)", command)
	}

	args, ok := got["args"].([]string)
	if !ok {
		t.Fatalf("args = %v (%T), want the argument slice", got["args"], got["args"])
	}
	if len(args) != 1 || args[0] != "echo hi" {
		t.Errorf("args = %v, want [echo hi]", args)
	}
}

// A service that sets only args — the common case, since a compose `command:`
// with no entrypoint override lands in Args — must report args and omit
// command entirely rather than reporting an empty one.
func TestServiceDetailsOmitsAnUnsetCommand(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Args: []string{"nginx", "-g", "daemon off;"}},
			},
		},
	}

	got := ServiceDetails(svc)

	if _, present := got["command"]; present {
		t.Errorf("command = %v, want the key absent when no entrypoint is set", got["command"])
	}
	if args, ok := got["args"].([]string); !ok || len(args) != 3 {
		t.Errorf("args = %v, want the three-element argument slice", got["args"])
	}
}

// TestServiceDetailsReportsHealthcheckDetail: a write answers with the section
// it changed, and for a healthcheck a bare bool cannot confirm what was just
// configured. Durations are strings, per the house rule — Docker's own type
// carries nanosecond integers, which read as arbitrary large numbers.
func TestServiceDetailsReportsHealthcheckDetail(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Healthcheck: &container.HealthConfig{
						Test:        []string{"CMD", "curl", "-f", "http://localhost/"},
						Interval:    10 * time.Second,
						Timeout:     3 * time.Second,
						StartPeriod: 30 * time.Second,
						Retries:     3,
					},
				},
			},
		},
	}

	got := ServiceDetails(svc)

	if got["healthcheck"] != true {
		t.Errorf("healthcheck = %v, want true", got["healthcheck"])
	}
	if got["healthcheckInterval"] != "10s" {
		t.Errorf("healthcheckInterval = %v, want \"10s\"", got["healthcheckInterval"])
	}
	if got["healthcheckTimeout"] != "3s" {
		t.Errorf("healthcheckTimeout = %v, want \"3s\"", got["healthcheckTimeout"])
	}
	if got["healthcheckStartPeriod"] != "30s" {
		t.Errorf("healthcheckStartPeriod = %v, want \"30s\"", got["healthcheckStartPeriod"])
	}
	if got["healthcheckRetries"] != 3 {
		t.Errorf("healthcheckRetries = %v, want 3", got["healthcheckRetries"])
	}

	test, ok := got["healthcheckTest"].([]string)
	if !ok || len(test) != 4 || test[0] != "CMD" {
		t.Errorf("healthcheckTest = %v, want the probe command", got["healthcheckTest"])
	}
}

// A service with no healthcheck reports false and carries none of the detail
// keys, so "not configured" is unambiguous rather than a set of zeroes.
func TestServiceDetailsOmitsHealthcheckDetailWhenUnset(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{}},
		},
	}

	got := ServiceDetails(svc)

	if got["healthcheck"] != false {
		t.Errorf("healthcheck = %v, want false", got["healthcheck"])
	}

	for _, key := range []string{
		"healthcheckTest",
		"healthcheckInterval",
		"healthcheckTimeout",
		"healthcheckStartPeriod",
		"healthcheckRetries",
	} {
		if _, present := got[key]; present {
			t.Errorf("%s present on a service with no healthcheck", key)
		}
	}
}
