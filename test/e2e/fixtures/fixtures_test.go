//go:build e2e

package fixtures_test

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
)

func TestDeployBaselineConverges(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	services, err := env.Docker.ServiceList(t.Context(), swarm.ServiceListOptions{})
	if err != nil {
		t.Fatalf("ServiceList: %v", err)
	}

	if len(services) < 4 {
		t.Errorf("service count = %d, want at least 4", len(services))
	}

	stacks := map[string]bool{}
	for _, svc := range services {
		stacks[svc.Spec.Labels["com.docker.stack.namespace"]] = true
	}

	for _, want := range []string{fixtures.StackShop, fixtures.StackPlatform} {
		if !stacks[want] {
			t.Errorf("stack %q missing; saw %v", want, stacks)
		}
	}
}

func TestDeployBaselineIsIdempotent(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	fixtures.DeployBaseline(t, env)
	before, err := env.Docker.ServiceList(t.Context(), swarm.ServiceListOptions{})
	if err != nil {
		t.Fatalf("ServiceList: %v", err)
	}

	fixtures.DeployBaseline(t, env)
	after, err := env.Docker.ServiceList(t.Context(), swarm.ServiceListOptions{})
	if err != nil {
		t.Fatalf("ServiceList: %v", err)
	}

	if len(before) != len(after) {
		t.Errorf("service count changed on redeploy: %d -> %d", len(before), len(after))
	}
}
