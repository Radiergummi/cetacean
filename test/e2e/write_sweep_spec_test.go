//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// Service spec-editing drivers for the write sweep, verified by reading the spec
// back off the engine rather than off Cetacean. A config, secret or network in
// use cannot be removed while a service holds it, so every case creates its
// target before the service, and LIFO cleanup tears the service down first.

// ─── attachments ────────────────────────────────────────────────────────

func driveServiceConfigs(t *testing.T, env *harness.Env, proc *sut.Process) {
	name := sweepName("sweep-svc-config")
	configID := engineConfig(t, env, name, nil)

	service := deployThrowawayService(t, env, "svcconfigs")
	id := serviceID(t, proc, service)

	awaitCached(t, proc, "/configs/"+configID)

	body, err := json.Marshal(map[string]any{
		"configs": []map[string]string{
			{"configID": configID, "configName": name, "fileName": "/etc/sweep.conf"},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/configs",
		"application/merge-patch+json", body,
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH configs: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)

	spec := svc.Spec.TaskTemplate.ContainerSpec
	if spec == nil || len(spec.Configs) != 1 {
		t.Fatalf("engine service has %v config references, want one", spec)
	}

	ref := spec.Configs[0]
	if ref.ConfigID != configID || ref.ConfigName != name {
		t.Errorf("config reference = %s/%s, want %s/%s",
			ref.ConfigID, ref.ConfigName, configID, name)
	}

	// The file target is what a config attachment is *for*: without it the
	// reference names a config the container never sees.
	if ref.File == nil || ref.File.Name != "/etc/sweep.conf" {
		t.Errorf("config file target = %+v, want /etc/sweep.conf", ref.File)
	}
}

func driveServiceSecrets(t *testing.T, env *harness.Env, proc *sut.Process) {
	name := sweepName("sweep-svc-secret")
	secretID := engineSecret(t, env, name, nil)

	service := deployThrowawayService(t, env, "svcsecrets")
	id := serviceID(t, proc, service)

	awaitCached(t, proc, "/secrets/"+secretID)

	body, err := json.Marshal(map[string]any{
		"secrets": []map[string]string{
			{"secretID": secretID, "secretName": name, "fileName": "/run/secrets/sweep"},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/secrets",
		"application/merge-patch+json", body,
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH secrets: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)

	spec := svc.Spec.TaskTemplate.ContainerSpec
	if spec == nil || len(spec.Secrets) != 1 {
		t.Fatalf("engine service has %v secret references, want one", spec)
	}

	ref := spec.Secrets[0]
	if ref.SecretID != secretID || ref.SecretName != name {
		t.Errorf("secret reference = %s/%s, want %s/%s",
			ref.SecretID, ref.SecretName, secretID, name)
	}

	if ref.File == nil || ref.File.Name != "/run/secrets/sweep" {
		t.Errorf("secret file target = %+v, want /run/secrets/sweep", ref.File)
	}
}

func driveServiceNetworks(t *testing.T, env *harness.Env, proc *sut.Process) {
	name := sweepName("sweep-svc-network")
	networkID := engineNetworkEventuallyRemovable(t, env, name)

	service := deployThrowawayService(t, env, "svcnetworks")
	id := serviceID(t, proc, service)

	awaitCached(t, proc, "/networks/"+networkID)

	body, err := json.Marshal(map[string]any{
		"networks": []map[string]any{
			{"target": networkID, "aliases": []string{"sweep-alias"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/networks",
		"application/merge-patch+json", body,
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH networks: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)

	attachments := svc.Spec.TaskTemplate.Networks
	index := slices.IndexFunc(attachments, func(a swarm.NetworkAttachmentConfig) bool {
		return a.Target == networkID || a.Target == name
	})

	if index < 0 {
		t.Fatalf("engine service networks = %+v, want an attachment to %s", attachments, name)
	}

	if !slices.Contains(attachments[index].Aliases, "sweep-alias") {
		t.Errorf("attachment aliases = %v, want sweep-alias",
			attachments[index].Aliases)
	}
}

func driveServiceMounts(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "svcmounts")
	id := serviceID(t, proc, service)

	// tmpfs rather than a volume or a bind: the mount is the thing under
	// test, and a named volume would add a second resource whose teardown
	// races the task still holding it.
	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/mounts",
		"application/merge-patch+json",
		[]byte(`{"mounts":[{"Type":"tmpfs","Target":"/sweep-scratch"}]}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH mounts: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)

	spec := svc.Spec.TaskTemplate.ContainerSpec
	if spec == nil || len(spec.Mounts) != 1 {
		t.Fatalf("engine service has %v mounts, want one", spec)
	}

	if spec.Mounts[0].Type != mount.TypeTmpfs || spec.Mounts[0].Target != "/sweep-scratch" {
		t.Errorf("engine mount = %+v, want a tmpfs at /sweep-scratch", spec.Mounts[0])
	}
}

func driveServiceContainerConfig(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "svccontainer")
	id := serviceID(t, proc, service)

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/container-config",
		"application/merge-patch+json",
		[]byte(`{"stopSignal":"SIGQUIT","user":"1000"}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH container-config: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)

	spec := svc.Spec.TaskTemplate.ContainerSpec
	if spec == nil {
		t.Fatal("engine service has no container spec")
	}

	if spec.StopSignal != "SIGQUIT" {
		t.Errorf("engine StopSignal = %q, want SIGQUIT", spec.StopSignal)
	}

	if spec.User != "1000" {
		t.Errorf("engine User = %q, want 1000", spec.User)
	}

	// The container spec carries the command the fixture was deployed with.
	// A merge patch that replaced the whole spec rather than merging into it
	// would drop it and still answer 200 — and the service would then run the
	// image's default command instead.
	if len(spec.Command) == 0 && len(spec.Args) == 0 {
		t.Error("the merge patch dropped the service's command and args")
	}
}

// ─── placement and modes ────────────────────────────────────────────────

func driveServicePlacement(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "svcplacement")
	id := serviceID(t, proc, service)

	// node.role==manager, not worker: the environment's sole node is the
	// cluster's only manager, so a worker constraint would leave the service
	// permanently unschedulable and the stack's teardown waiting on tasks
	// that never start.
	resp := sweepRequest(
		t, proc, http.MethodPut, "/services/"+id+"/placement",
		"application/json", []byte(`{"Constraints":["node.role==manager"]}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT placement: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)

	placement := svc.Spec.TaskTemplate.Placement
	if placement == nil || !slices.Contains(placement.Constraints, "node.role==manager") {
		t.Errorf("engine placement = %+v, want the node.role==manager constraint", placement)
	}
}

func driveServiceEndpointMode(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "svcendpoint")
	id := serviceID(t, proc, service)

	resp := sweepRequest(
		t, proc, http.MethodPut, "/services/"+id+"/endpoint-mode",
		"application/json", []byte(`{"mode":"dnsrr"}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT endpoint-mode: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)

	if svc.Spec.EndpointSpec == nil || svc.Spec.EndpointSpec.Mode != swarm.ResolutionModeDNSRR {
		t.Errorf("engine endpoint spec = %+v, want dnsrr", svc.Spec.EndpointSpec)
	}
}

// ─── healthcheck ────────────────────────────────────────────────────────

func driveServiceHealthcheckPut(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "svchcput")
	id := serviceID(t, proc, service)

	// Durations are nanoseconds on the wire, matching container.HealthConfig.
	resp := sweepRequest(
		t, proc, http.MethodPut, "/services/"+id+"/healthcheck",
		"application/json",
		[]byte(`{"Test":["CMD-SHELL","true"],"Interval":10000000000,`+
			`"Timeout":5000000000,"Retries":3}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT healthcheck: status = %d, want 200", resp.StatusCode)
	}

	health := engineHealthcheck(t, env, service)

	if len(health.Test) < 2 || health.Test[0] != "CMD-SHELL" {
		t.Errorf("engine healthcheck test = %v, want a CMD-SHELL probe", health.Test)
	}

	if health.Retries != 3 {
		t.Errorf("engine healthcheck retries = %d, want 3", health.Retries)
	}

	if health.Interval != 10*time.Second {
		t.Errorf("engine healthcheck interval = %s, want 10s", health.Interval)
	}
}

func driveServiceHealthcheckPatch(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "svchcpatch")
	id := serviceID(t, proc, service)

	// A PUT first, so the PATCH has something to merge into — that is the
	// distinction between the two routes, and patching an absent healthcheck
	// would not show it.
	put := sweepRequest(
		t, proc, http.MethodPut, "/services/"+id+"/healthcheck",
		"application/json",
		[]byte(`{"Test":["CMD-SHELL","true"],"Interval":10000000000,`+
			`"Timeout":5000000000,"Retries":3}`),
	)
	putStatus := put.StatusCode
	put.Body.Close()

	if putStatus != http.StatusOK {
		t.Fatalf("PUT healthcheck (setup): status = %d, want 200", putStatus)
	}

	resp := sweepWriteAfterWrite(
		t, proc, http.MethodPatch, "/services/"+id+"/healthcheck",
		"application/merge-patch+json", []byte(`{"Retries":5}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH healthcheck: status = %d, want 200", resp.StatusCode)
	}

	health := engineHealthcheck(t, env, service)

	if health.Retries != 5 {
		t.Errorf("engine healthcheck retries = %d, want the patched 5", health.Retries)
	}

	// The merge, not the write, is what this route adds over the PUT.
	if len(health.Test) < 2 || health.Test[0] != "CMD-SHELL" {
		t.Errorf("engine healthcheck test = %v; the merge patch dropped the probe",
			health.Test)
	}

	if health.Interval != 10*time.Second {
		t.Errorf("engine healthcheck interval = %s; the merge patch dropped it",
			health.Interval)
	}
}

// engineHealthcheck reads a service's healthcheck straight off the engine.
func engineHealthcheck(
	t *testing.T,
	env *harness.Env,
	service string,
) *container.HealthConfig {
	t.Helper()

	svc := inspectService(t, env, service)

	spec := svc.Spec.TaskTemplate.ContainerSpec
	if spec == nil || spec.Healthcheck == nil {
		t.Fatalf("engine service %s carries no healthcheck", service)
	}

	return spec.Healthcheck
}

// engineNetworkEventuallyRemovable is engineNetwork with a cleanup that rides
// out endpoint detachment: an overlay network stays busy for a moment after the
// service attached to it is gone, and a single NetworkRemove in that window
// fails with a conflict, leaking a name the next run would collide with.
func engineNetworkEventuallyRemovable(t *testing.T, env *harness.Env, name string) string {
	t.Helper()

	created, err := env.Docker.NetworkCreate(context.Background(), name, network.CreateOptions{
		Driver:     "overlay",
		Attachable: true,
	})
	if err != nil {
		t.Fatalf("NetworkCreate %s: %v", name, err)
	}

	t.Cleanup(func() {
		deadline := time.Now().Add(60 * time.Second)

		for {
			err := env.Docker.NetworkRemove(context.Background(), created.ID)
			if err == nil || cerrdefs.IsNotFound(err) {
				return
			}

			if time.Now().After(deadline) {
				t.Errorf("cleanup: NetworkRemove %s: %v", name, err)

				return
			}

			time.Sleep(time.Second)
		}
	})

	return created.ID
}

// Drives the merge-patch contract across the write the cache cannot see: a field
// written moments earlier must survive a patch that never mentions it. A merge
// base from the asynchronously filled cache cannot do that, and nothing catches
// it — the engine accepts the lost update with a 200.
func TestWriteSweepMergePatchMergesAgainstTheLiveSpec(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startWriteSweep(t, env, "3")

	service := deployThrowawayService(t, env, "stalemerge")
	id := serviceID(t, proc, service)

	// Write a healthcheck, then immediately patch one field of it. A client
	// doing two writes in a row — a script, an agent, a form that saves
	// section by section — produces exactly this sequence.
	put := sweepRequest(
		t, proc, http.MethodPut, "/services/"+id+"/healthcheck",
		"application/json",
		[]byte(`{"Test":["CMD-SHELL","true"],"Interval":10000000000,`+
			`"Timeout":5000000000,"Retries":3}`),
	)
	putStatus := put.StatusCode
	put.Body.Close()

	if putStatus != http.StatusOK {
		t.Fatalf("PUT healthcheck: status = %d, want 200", putStatus)
	}

	resp := sweepWriteAfterWrite(
		t, proc, http.MethodPatch, "/services/"+id+"/healthcheck",
		"application/merge-patch+json", []byte(`{"Retries":5}`),
	)
	patchStatus := resp.StatusCode
	patchBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if patchStatus != http.StatusOK {
		t.Fatalf("PATCH healthcheck: status = %d, want 200; body: %s", patchStatus, patchBody)
	}

	svc := inspectService(t, env, service)

	spec := svc.Spec.TaskTemplate.ContainerSpec
	if spec == nil {
		t.Fatal("engine service has no container spec")
	}

	health := spec.Healthcheck
	if health == nil {
		t.Fatalf(
			"the engine has no healthcheck at all after a PUT that set one and a " +
				"PATCH that only touched Retries",
		)
	}

	if health.Retries != 5 {
		t.Errorf("engine retries = %d, want the patched 5", health.Retries)
	}

	if len(health.Test) == 0 || health.Interval != 10*time.Second {
		t.Errorf(
			"a merge patch naming only Retries left the engine with Test=%v and "+
				"Interval=%s, discarding what the immediately preceding PUT wrote",
			health.Test, health.Interval,
		)
	}
}
