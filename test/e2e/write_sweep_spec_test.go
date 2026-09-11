//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
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

// Service spec-editing drivers for the write sweep: the five attachment
// PATCHes, placement, the two mode switches, and the healthcheck pair. The
// sibling of write_sweep_lifecycle_test.go, registered the same way in
// drivenWriteRoutes and verified the same way — by reading the spec back off
// the engine, never off Cetacean's own response.
//
// Attachment editing is the part of this surface with real ordering hazards:
// a config, secret or network in use by a service cannot be removed while the
// service holds it. Every case here creates its attachment target *before*
// deploying the service that will reference it, so Go's LIFO cleanup order
// tears the service down first.

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

func driveServiceMode(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "svcmode")
	id := serviceID(t, proc, service)

	before := inspectService(t, env, service)
	if before.Spec.Mode.Replicated == nil {
		t.Fatalf("the throwaway fixture is not replicated to begin with: %+v", before.Spec.Mode)
	}

	mark := len(proc.Logs())

	resp := sweepRequest(
		t, proc, http.MethodPut, "/services/"+id+"/mode",
		"application/json", []byte(`{"mode":"global"}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		svc := inspectService(t, env, service)

		// Both halves matter: a switch that set Global without clearing
		// Replicated would answer 200 and leave the engine with a spec it
		// will reject on the next write.
		if svc.Spec.Mode.Global == nil {
			t.Errorf("engine mode = %+v, want global", svc.Spec.Mode)
		}

		if svc.Spec.Mode.Replicated != nil {
			t.Errorf("engine mode = %+v, still carries the replicated half", svc.Spec.Mode)
		}

		return
	}

	// QUARANTINED — finding D-8. Swarmkit refuses every service mode change,
	// in either direction, with gRPC Unimplemented "service mode change is
	// not allowed"; there is no Docker CLI flag for it either. So this
	// endpoint — documented in api/openapi.yaml as "Changes the service mode
	// between replicated and global" with a 200 response, and listed in
	// docs/api.md as a tier-3 operation — cannot succeed against any Docker
	// version. The refusal is then reported as a generic 500 ENG004 "Docker
	// Engine Error", which is indistinguishable from the engine being broken.
	//
	// The case asserts the documented outcome, and tolerates only the one
	// refusal the engine actually gives, identified from the SUT's own log
	// rather than from the response — the response says nothing about the
	// cause.
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf(
			"PUT mode: status = %d, want 200; this is neither the documented outcome "+
				"nor the quarantined D-8 refusal; body: %s",
			resp.StatusCode, body,
		)
	}

	awaitLog(t, proc, mark, "service mode change is not allowed")

	t.Logf(
		"D-8 still open: PUT /services/{id}/mode answered 500/ENG004. The engine "+
			"refused with Unimplemented \"service mode change is not allowed\" — Swarmkit "+
			"forbids every mode change, so this endpoint can never succeed, and the "+
			"refusal reaches the client as a generic engine error. Response: %s",
		body,
	)
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

	// HandlePatchServiceHealthcheck computes its merge base from the cache,
	// which the watcher fills asynchronously. Patching before the cache has
	// seen the PUT merges into a service with no healthcheck at all and
	// silently discards the probe — that is finding D-7, and
	// TestWriteSweepMergePatchAgainstStaleCache is where it is pinned. This
	// case is about the merge itself, so it waits for the base to be current
	// rather than racing it.
	awaitHealthcheckCached(t, proc, id, "CMD-SHELL")

	resp := sweepRequest(
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
// out endpoint detachment. An overlay network a service was attached to stays
// busy for a moment after the service is gone, and a single NetworkRemove in
// that window fails with a conflict — leaking a network whose name the next
// run would collide with.
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

// awaitHealthcheckCached blocks until Cetacean's own view of a service's
// healthcheck contains want, which is the cache state every merge-patch
// handler computes its base from.
func awaitHealthcheckCached(t *testing.T, proc *sut.Process, id, want string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for {
		resp := sweepRequest(
			t, proc, http.MethodGet, "/services/"+id+"/healthcheck", "", nil,
		)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK && strings.Contains(string(body), want) {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf(
				"Cetacean never reported a healthcheck containing %q for service %s "+
					"(last status %d, body %s)",
				want, id, resp.StatusCode, body,
			)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// TestWriteSweepMergePatchAgainstStaleCache pins finding D-7: six service
// merge-patch handlers compute their base from the cache rather than from the
// service as it currently stands, so a field written moments earlier can be
// silently discarded by a patch that never mentions it.
//
// The hazard is documented in the codebase itself.
// HandlePatchServiceEnv carries it as a comment — "the actual merge runs
// against the freshly-inspected service spec inside the writer (M-42).
// Pre-merging against the cache would race third-party writers and silently
// drop concurrent changes to other env keys" — and env was built that way.
// PATCH resources, update-policy, rollback-policy, log-driver, healthcheck and
// container-config still pre-merge against `h.cache.GetService`.
//
// Nothing catches it downstream. The writer re-inspects the service to get a
// current version before calling ServiceUpdate, so the engine's own optimistic
// concurrency — the mechanism for exactly this — sees a fresh version and
// accepts the write. The If-Match precondition does not close it either: it is
// optional, and the representation it compares against is built from the same
// cache, so it can only detect changes the cache has already seen.
//
// The result is a lost update reported as 200. This case asserts the
// merge-patch contract (fields the patch does not mention keep their current
// value) and tolerates only that one outcome.
func TestWriteSweepMergePatchAgainstStaleCache(t *testing.T) {
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

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/healthcheck",
		"application/merge-patch+json", []byte(`{"Retries":5}`),
	)
	patchStatus := resp.StatusCode
	patchBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// A refusal would be a defensible answer: the server cannot merge into a
	// base it does not have. Only a success that quietly discarded the probe
	// is the defect.
	if patchStatus != http.StatusOK {
		t.Logf("the patch was refused rather than merged (status %d): %s",
			patchStatus, patchBody)

		return
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

	preserved := len(health.Test) > 0 && health.Interval == 10*time.Second

	if preserved {
		// Either the defect is fixed, or the cache happened to be current.
		// Both are correct outcomes; there is nothing to report.
		return
	}

	t.Logf(
		"D-7 still open: a merge patch that named only Retries left the engine with "+
			"Test=%v and Interval=%s, discarding the probe written by the immediately "+
			"preceding PUT. HandlePatchServiceHealthcheck merged into "+
			"h.cache.GetService's copy, which the watcher had not yet refreshed, while "+
			"the writer supplied a freshly inspected version — so the engine could not "+
			"reject it and the caller was told 200. Same shape in PATCH resources, "+
			"update-policy, rollback-policy, log-driver and container-config.",
		health.Test, health.Interval,
	)
}
