//go:build e2e

package fixtures_test

import (
	"testing"

	"github.com/docker/docker/api/types/network"
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

// TestDeployBaselineIsIdempotent removes the completion sentinel before the
// second call, which is what makes the test mean anything: with the sentinel
// in place both calls short-circuit in baselinePresent and the assertion
// compares two ServiceList calls with no operation between them. Deleting it
// forces the re-drive over resources that already exist, which is both the
// recovery path a partially-failed run depends on and the only exercise the
// create helpers' conflict tolerance gets.
func TestDeployBaselineIsIdempotent(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	fixtures.DeployBaseline(t, env)

	before, err := env.Docker.ServiceList(t.Context(), swarm.ServiceListOptions{})
	if err != nil {
		t.Fatalf("ServiceList: %v", err)
	}

	if err := env.Docker.ConfigRemove(t.Context(), fixtures.BaselineSentinel); err != nil {
		t.Fatalf("remove sentinel %s: %v", fixtures.BaselineSentinel, err)
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

// TestBaselineShopWebUsesItsResources asserts the used-vs-orphan contrast the
// baseline is designed around is real: shop_web must actually reference the
// shop config, secret, network and volume, not merely coexist with them. A
// test that only counted services would not catch four resources that are
// created but never attached to anything.
func TestBaselineShopWebUsesItsResources(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	svc, _, err := env.Docker.ServiceInspectWithRaw(
		t.Context(),
		"shop_web",
		swarm.ServiceInspectOptions{},
	)
	if err != nil {
		t.Fatalf("ServiceInspectWithRaw shop_web: %v", err)
	}

	containerSpec := svc.Spec.TaskTemplate.ContainerSpec
	if containerSpec == nil {
		t.Fatalf("shop_web has no container spec")
	}

	configNames := map[string]bool{}
	for _, ref := range containerSpec.Configs {
		configNames[ref.ConfigName] = true
	}

	if !configNames["shop-config"] {
		t.Errorf("shop_web configs = %v, want shop-config", configNames)
	}

	secretNames := map[string]bool{}
	for _, ref := range containerSpec.Secrets {
		secretNames[ref.SecretName] = true
	}

	if !secretNames["shop-secret"] {
		t.Errorf("shop_web secrets = %v, want shop-secret", secretNames)
	}

	// The daemon normalizes a network attachment's Target to the network's
	// ID, so resolve shop-net's ID rather than comparing against its name.
	shopNet, err := env.Docker.NetworkInspect(t.Context(), "shop-net", network.InspectOptions{})
	if err != nil {
		t.Fatalf("NetworkInspect shop-net: %v", err)
	}

	networkTargets := map[string]bool{}
	for _, attachment := range svc.Spec.TaskTemplate.Networks {
		networkTargets[attachment.Target] = true
	}

	if !networkTargets[shopNet.ID] {
		t.Errorf("shop_web networks = %v, want shop-net (%s)", networkTargets, shopNet.ID)
	}

	volumeMounted := false

	for _, m := range containerSpec.Mounts {
		if m.Source == fixtures.UsedVolume && m.Target == "/data" {
			volumeMounted = true
		}
	}

	if !volumeMounted {
		t.Errorf(
			"shop_web mounts = %v, want %s at /data",
			containerSpec.Mounts,
			fixtures.UsedVolume,
		)
	}
}
