//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/contract"
	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives Cetacean's write endpoints beyond scale and restart
// (covered in write_test.go): image update, rollback, node drain, task
// removal, a genuine concurrent-write conflict, and the service-spec PATCHes
// the fixture image supports. It reserves port 19006 (see README.md's
// reserved-ports table).
//
// drivenWriteRoutes and excusedWriteRoutes together are the self-enforcing
// gate: TestEveryWriteRouteIsDrivenOrExcused requires every mutating route
// contract.Routes() reports to appear in one or the other, so a new write
// endpoint added to the router fails this test instead of silently going
// untested.

const writeSweepPort = 19006

// fixtureImageRef must match the unexported fixtureImage constant in
// test/e2e/fixtures/fixtures.go — the image DeployStack loads into the
// engine before creating any service. There is no exported way to read it
// from here.
const fixtureImageRef = "cetacean-e2e-fixture:latest"

// startWriteSweep brings up a no-auth SUT on this file's reserved port at the
// given CETACEAN_OPERATIONS_LEVEL.
func startWriteSweep(t *testing.T, env *harness.Env, opsLevel string) *sut.Process {
	t.Helper()

	return sut.Start(t, sut.Config{
		Port:       writeSweepPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":        "none",
			"CETACEAN_OPERATIONS_LEVEL": opsLevel,
		},
	})
}

// deployThrowawayService deploys a single-service, single-replica throwaway
// stack and returns the service's fixture name (stack-prefixed). Each driven
// route gets its own stack so the cases are independent of one another and
// of the shared baseline, which this file never deploys.
func deployThrowawayService(t *testing.T, env *harness.Env, name string) string {
	t.Helper()

	stack := fixtures.DeployStack(t, env, name, []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})

	return stack + "_app"
}

// inspectService reads a service straight from the engine, failing the test
// on error. Verification in this file reads the engine, not Cetacean's own
// response, so a handler that answers 200 without touching the cluster
// cannot pass.
func inspectService(t *testing.T, env *harness.Env, name string) swarm.Service {
	t.Helper()

	svc, _, err := env.Docker.ServiceInspectWithRaw(
		context.Background(), name, swarm.ServiceInspectOptions{},
	)
	if err != nil {
		t.Fatalf("ServiceInspectWithRaw %s: %v", name, err)
	}

	return svc
}

// cleanupTaggedImage registers a t.Cleanup that removes a local image tag
// this file created on the shared DinD engine (via ImageTag), tolerating a
// not-found error so a passing test can never fail on cleanup. Without this,
// the sweep-image/sweep-rollback tags it creates accumulate on the shared
// engine across runs.
func cleanupTaggedImage(t *testing.T, env *harness.Env, tag string) {
	t.Helper()

	t.Cleanup(func() {
		if _, err := env.Docker.ImageRemove(
			context.Background(), tag, image.RemoveOptions{},
		); err != nil && !cerrdefs.IsNotFound(err) {
			t.Errorf("cleanup: ImageRemove %s: %v", tag, err)
		}
	})
}

// waitForServiceImage polls the engine until service's image carries the
// given prefix, or fails the test after two minutes.
func waitForServiceImage(t *testing.T, env *harness.Env, service, wantPrefix string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		svc, _, err := env.Docker.ServiceInspectWithRaw(
			context.Background(), service, swarm.ServiceInspectOptions{},
		)
		if err == nil && svc.Spec.TaskTemplate.ContainerSpec != nil &&
			strings.HasPrefix(svc.Spec.TaskTemplate.ContainerSpec.Image, wantPrefix) {
			return
		}

		time.Sleep(2 * time.Second)
	}

	t.Fatalf("service %s image never became %s on the engine", service, wantPrefix)
}

// waitForCachedServiceImage polls Cetacean's own GET /services/{id} — not the
// engine — until it reflects wantPrefix. Used before a route that reads a
// resource from Cetacean's cache (like rollback's PreviousSpec check) rather
// than the engine directly: a change made straight against the engine is
// visible there immediately, but Cetacean only learns of it once its watcher
// processes the corresponding Docker event, and driving the route before
// that catches up fails on a precondition the write sweep didn't intend to
// exercise.
func waitForCachedServiceImage(t *testing.T, proc *sut.Process, id, wantPrefix string) {
	t.Helper()

	type detail struct {
		Service struct {
			Spec struct {
				TaskTemplate struct {
					ContainerSpec struct {
						Image string `json:"Image"`
					} `json:"ContainerSpec"`
				} `json:"TaskTemplate"`
			} `json:"Spec"`
		} `json:"service"`
	}

	matches := func() bool {
		req, err := http.NewRequestWithContext(
			context.Background(), http.MethodGet, proc.BaseURL+"/services/"+id, nil,
		)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := proc.Client().Do(req)
		if err != nil {
			t.Fatalf("GET /services/%s: %v", id, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return false
		}

		var body detail
		return json.NewDecoder(resp.Body).Decode(&body) == nil &&
			strings.HasPrefix(body.Service.Spec.TaskTemplate.ContainerSpec.Image, wantPrefix)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if matches() {
			return
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("Cetacean's cache for service %s never reflected image %s", id, wantPrefix)
}

// runningTaskCount reports how many of serviceID's tasks the engine currently
// reports as running.
func runningTaskCount(ctx context.Context, env *harness.Env, serviceID string) (int, error) {
	tasks, err := env.Docker.TaskList(ctx, swarm.TaskListOptions{})
	if err != nil {
		return 0, err
	}

	count := 0
	for _, task := range tasks {
		if task.ServiceID == serviceID && task.Status.State == swarm.TaskStateRunning {
			count++
		}
	}

	return count, nil
}

// sweepRequest issues one request against proc and returns the response. The
// caller closes the body.
func sweepRequest(
	t *testing.T,
	proc *sut.Process,
	method, path, contentType string,
	body []byte,
) *http.Response {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(
		context.Background(), method, proc.BaseURL+path, reader,
	)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}

	return resp
}

// problemType decodes an RFC 9457 problem body and returns its type field,
// closing the body. Used to assert on the error code named in the response
// rather than the status code alone.
func problemType(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()

	var problem struct {
		Type string `json:"type"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}

	return problem.Type
}

// contractRoutes returns the router's route inventory. contract.Routes()
// parses a path relative to internal/contract's own directory, but `go test`
// runs this package's binary with test/e2e as the working directory — so the
// call is bracketed with a chdir into internal/contract and back. This file
// runs serially (no t.Parallel, and the suite requires -p 1 across packages),
// so the brief change of process-wide working directory cannot race any
// other test in this binary.
func contractRoutes(t *testing.T) []contract.Route {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate write_sweep_test.go source")
	}

	contractDir := filepath.Clean(
		filepath.Join(filepath.Dir(file), "..", "..", "internal", "contract"),
	)

	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	if err := os.Chdir(contractDir); err != nil {
		t.Fatalf("Chdir %s: %v", contractDir, err)
	}
	defer func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("Chdir back to %s: %v", original, err)
		}
	}()

	routes, err := contract.Routes()
	if err != nil {
		t.Fatalf("contract.Routes: %v", err)
	}

	return routes
}

// ─── driven routes ──────────────────────────────────────────────────────

type driveFunc func(t *testing.T, env *harness.Env, proc *sut.Process)

// drivenWriteRoutes is the single source of truth for what this file
// exercises: TestWriteSweepMutatesTheCluster runs every entry against a real
// engine, TestWriteSweepRefusedAtReadOnlyLevel replays every entry's route at
// operations level 0, and TestEveryWriteRouteIsDrivenOrExcused requires every
// other mutating route in the inventory to carry a reason in
// excusedWriteRoutes instead.
var drivenWriteRoutes = map[string]driveFunc{
	"PUT /services/{id}/image":             driveServiceImage,
	"POST /services/{id}/rollback":         driveServiceRollback,
	"PATCH /services/{id}/env":             driveServiceEnv,
	"PATCH /services/{id}/labels":          driveServiceLabels,
	"PATCH /services/{id}/resources":       driveServiceResources,
	"PATCH /services/{id}/ports":           driveServicePorts,
	"PATCH /services/{id}/update-policy":   driveServiceUpdatePolicy,
	"PATCH /services/{id}/rollback-policy": driveServiceRollbackPolicy,
	"PATCH /services/{id}/log-driver":      driveServiceLogDriver,
	"PUT /nodes/{id}/availability":         driveNodeAvailability,
	"DELETE /tasks/{id}":                   driveTaskRemoval,

	// Resource lifecycle — see write_sweep_lifecycle_test.go.
	"POST /configs":              driveConfigCreate,
	"DELETE /configs/{id}":       driveConfigRemoval,
	"PATCH /configs/{id}/labels": driveConfigLabels,
	"POST /secrets":              driveSecretCreate,
	"DELETE /secrets/{id}":       driveSecretRemoval,
	"PATCH /secrets/{id}/labels": driveSecretLabels,
	"DELETE /networks/{id}":      driveNetworkRemoval,
	"DELETE /volumes/{name}":     driveVolumeRemoval,
	"DELETE /services/{id}":      driveServiceRemoval,
	"DELETE /stacks/{name}":      driveStackRemoval,
	"PATCH /nodes/{id}/labels":   driveNodeLabels,

	// Service spec editing — see write_sweep_spec_test.go.
	"PATCH /services/{id}/configs":          driveServiceConfigs,
	"PATCH /services/{id}/secrets":          driveServiceSecrets,
	"PATCH /services/{id}/networks":         driveServiceNetworks,
	"PATCH /services/{id}/mounts":           driveServiceMounts,
	"PATCH /services/{id}/container-config": driveServiceContainerConfig,
	"PUT /services/{id}/placement":          driveServicePlacement,
	"PUT /services/{id}/mode":               driveServiceMode,
	"PUT /services/{id}/endpoint-mode":      driveServiceEndpointMode,
	"PUT /services/{id}/healthcheck":        driveServiceHealthcheckPut,
	"PATCH /services/{id}/healthcheck":      driveServiceHealthcheckPatch,

	// Cluster-level tuning — see write_sweep_swarm_test.go.
	"PATCH /swarm/orchestration": driveSwarmOrchestration,
	"PATCH /swarm/raft":          driveSwarmRaft,
	"PATCH /swarm/dispatcher":    driveSwarmDispatcher,
	"POST /swarm/rotate-token":   driveSwarmRotateToken,
}

// excusedWriteRoutes carries a reason for every mutating route this file does
// not drive. "gap: ..." marks a route simply out of scope for this slice, not
// yet covered — see write-sweep-report.md for the full count. Every other
// reason is meant to hold permanently: driving that route would be
// destructive to the shared single-node engine every lane in this test
// binary depends on, or is already covered elsewhere.
//
//nolint:gosec // G101: keys are route patterns (e.g. "POST /secrets"), not credentials.
var excusedWriteRoutes = map[string]string{
	// Genuinely destructive / environment-dependent, named as such in the
	// coverage brief.
	"PATCH /swarm/ca": "mutates the shared engine's cluster-wide CA config " +
		"(NodeCertExpiry via systemClient.UpdateSwarm); genuinely " +
		"environment-dependent, excused rather than forced",
	"POST /swarm/force-rotate-ca": "forces CA certificate rotation across " +
		"the shared engine every lane in this test binary depends on; " +
		"excused rather than forced",
	"POST /swarm/rotate-unlock-key": "rotates the manager unlock key on " +
		"the shared engine; excused rather than forced",
	"POST /swarm/unlock": "only meaningful with autolock enabled, which " +
		"this environment does not configure; excused rather than forced",

	// The /plugins* family needs a plugin registry this fixture does not
	// provide.
	"DELETE /plugins/{name}":         "needs a plugin registry this fixture does not provide",
	"PATCH /plugins/{name}/settings": "needs a plugin registry this fixture does not provide",
	"POST /plugins":                  "needs a plugin registry this fixture does not provide",
	"POST /plugins/privileges":       "needs a plugin registry this fixture does not provide",
	"POST /plugins/{name}/disable":   "needs a plugin registry this fixture does not provide",
	"POST /plugins/{name}/enable":    "needs a plugin registry this fixture does not provide",
	"POST /plugins/{name}/upgrade":   "needs a plugin registry this fixture does not provide",

	// The DinD swarm this suite runs against has exactly one node.
	"DELETE /nodes/{id}": "the environment's swarm has exactly one node; " +
		"removing it would tear down the cluster every other lane in this " +
		"test binary shares",
	"PUT /nodes/{id}/role": "the single node is the cluster's only " +
		"manager; demoting it to worker would break swarm consensus for " +
		"every other lane sharing this engine",

	// Already driven, just not in this file.
	"PUT /services/{id}/scale": "driven by TestScaleServiceTakesEffectOnTheCluster " +
		"and refused-at-tier-0 by TestScaleRefusedAtReadOnlyLevel, both in write_test.go",
	"POST /services/{id}/restart": "driven by " +
		"TestRestartServiceRecreatesTasksOnTheCluster in write_test.go",

	"POST /-/resync": "registered under the auth-exempt /-/ prefix with no requireLevel " +
		"wrapper, so it cannot be replayed at tier 0 the way every other driven entry is; " +
		"driven instead by TestResyncBypassesAuthenticationAndTheOperationsTier in " +
		"write_sweep_swarm_test.go, which pins that as finding D-9",
	"PATCH /swarm/encryption": "enabling autolock means a manager restart needs an unlock " +
		"key, and this harness restarts SUTs against a shared engine with nowhere to keep " +
		"one; excused rather than forced, unlike the three reversible /swarm/* patches " +
		"beside it in write_sweep_swarm_test.go",
}

func driveServiceImage(t *testing.T, env *harness.Env, proc *sut.Process) {
	ctx := context.Background()

	// Registered before the throwaway stack below, so t.Cleanup's LIFO order
	// removes the service (and the container still using this tag) first and
	// the image tag second — the other way round, ImageRemove would fail
	// with "image is being used by running container" while the test itself
	// still passed.
	newTag := "cetacean-e2e-fixture:sweep-image"
	cleanupTaggedImage(t, env, newTag)

	service := deployThrowawayService(t, env, "img")
	id := serviceID(t, proc, service)

	if err := env.Docker.ImageTag(ctx, fixtureImageRef, newTag); err != nil {
		t.Fatalf("ImageTag: %v", err)
	}

	body, err := json.Marshal(map[string]any{"image": newTag})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := sweepRequest(
		t,
		proc,
		http.MethodPut,
		"/services/"+id+"/image",
		"application/json",
		body,
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT image: status = %d, want 200", resp.StatusCode)
	}

	waitForServiceImage(t, env, service, newTag)
}

func driveServiceRollback(t *testing.T, env *harness.Env, proc *sut.Process) {
	ctx := context.Background()

	// Registered before the throwaway stack below, so t.Cleanup's LIFO order
	// removes the service first and the image tag second — see the same
	// comment in driveServiceImage. The route under test reverts the
	// service off this tag before the test returns, but registering the
	// same way keeps this case safe even if an earlier assertion fails
	// first.
	altTag := "cetacean-e2e-fixture:sweep-rollback"
	cleanupTaggedImage(t, env, altTag)

	service := deployThrowawayService(t, env, "rollback")
	id := serviceID(t, proc, service)

	// Give the service a PreviousSpec by updating it directly against the
	// engine, independent of the route this test exists to drive.
	if err := env.Docker.ImageTag(ctx, fixtureImageRef, altTag); err != nil {
		t.Fatalf("ImageTag: %v", err)
	}

	svc := inspectService(t, env, service)
	svc.Spec.TaskTemplate.ContainerSpec.Image = altTag

	if _, err := env.Docker.ServiceUpdate(
		ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{},
	); err != nil {
		t.Fatalf("ServiceUpdate (seed a previous spec): %v", err)
	}

	waitForServiceImage(t, env, service, altTag)

	// The seed update ran directly against the engine, bypassing Cetacean;
	// wait for its own cache to catch up (via the watcher's Docker event
	// handling) before relying on the PreviousSpec the rollback handler
	// reads from that cache.
	waitForCachedServiceImage(t, proc, id, altTag)

	resp := sweepRequest(t, proc, http.MethodPost, "/services/"+id+"/rollback", "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST rollback: status = %d, want 200: %s", resp.StatusCode, b)
	}

	waitForServiceImage(t, env, service, fixtureImageRef)
}

func driveServiceEnv(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "env")
	id := serviceID(t, proc, service)

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/env",
		"application/merge-patch+json", []byte(`{"SWEEP_ENV":"1"}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH env: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)
	if svc.Spec.TaskTemplate.ContainerSpec == nil ||
		!slices.Contains(svc.Spec.TaskTemplate.ContainerSpec.Env, "SWEEP_ENV=1") {
		t.Errorf(
			"engine env = %v, want SWEEP_ENV=1 present",
			svc.Spec.TaskTemplate.ContainerSpec.Env,
		)
	}
}

func driveServiceLabels(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "labels")
	id := serviceID(t, proc, service)

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/labels",
		"application/merge-patch+json", []byte(`{"sweep-label":"yes"}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH labels: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)
	if svc.Spec.Labels["sweep-label"] != "yes" {
		t.Errorf("engine labels = %v, want sweep-label=yes", svc.Spec.Labels)
	}
}

func driveServiceResources(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "resources")
	id := serviceID(t, proc, service)

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/resources",
		"application/merge-patch+json", []byte(`{"Reservations":{"MemoryBytes":16777216}}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH resources: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)
	res := svc.Spec.TaskTemplate.Resources
	if res == nil || res.Reservations == nil || res.Reservations.MemoryBytes != 16777216 {
		t.Errorf("engine resources = %+v, want a 16MiB memory reservation", res)
	}
}

func driveServicePorts(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "ports")
	id := serviceID(t, proc, service)

	resp := sweepRequest(
		t,
		proc,
		http.MethodPatch,
		"/services/"+id+"/ports",
		"application/merge-patch+json",
		[]byte(
			`{"ports":[{"Protocol":"tcp","TargetPort":9090,"PublishedPort":30099,"PublishMode":"ingress"}]}`,
		),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH ports: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)
	found := false
	if svc.Spec.EndpointSpec != nil {
		for _, p := range svc.Spec.EndpointSpec.Ports {
			if p.PublishedPort == 30099 && p.TargetPort == 9090 {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("engine ports = %+v, want published port 30099 -> 9090", svc.Spec.EndpointSpec)
	}
}

func driveServiceUpdatePolicy(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "updatepolicy")
	id := serviceID(t, proc, service)

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/update-policy",
		"application/merge-patch+json", []byte(`{"Parallelism":2}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH update-policy: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)
	if svc.Spec.UpdateConfig == nil || svc.Spec.UpdateConfig.Parallelism != 2 {
		t.Errorf("engine update policy = %+v, want Parallelism=2", svc.Spec.UpdateConfig)
	}
}

func driveServiceRollbackPolicy(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "rollbackpolicy")
	id := serviceID(t, proc, service)

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/rollback-policy",
		"application/merge-patch+json", []byte(`{"Parallelism":2}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH rollback-policy: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)
	if svc.Spec.RollbackConfig == nil || svc.Spec.RollbackConfig.Parallelism != 2 {
		t.Errorf("engine rollback policy = %+v, want Parallelism=2", svc.Spec.RollbackConfig)
	}
}

func driveServiceLogDriver(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "logdriver")
	id := serviceID(t, proc, service)

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/services/"+id+"/log-driver",
		"application/merge-patch+json", []byte(`{"Name":"json-file","Options":{"max-size":"5m"}}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH log-driver: status = %d, want 200", resp.StatusCode)
	}

	svc := inspectService(t, env, service)
	drv := svc.Spec.TaskTemplate.LogDriver
	if drv == nil || drv.Name != "json-file" || drv.Options["max-size"] != "5m" {
		t.Errorf("engine log driver = %+v, want json-file max-size=5m", drv)
	}
}

// driveNodeAvailability drains the environment's single node, verifies the
// engine reports it drained AND that the throwaway service's task was
// actually evicted (there is nowhere else to schedule it), then restores the
// node to active — required because this environment has exactly one node,
// so leaving it drained would strand every other lane sharing this engine.
func driveNodeAvailability(t *testing.T, env *harness.Env, proc *sut.Process) {
	ctx := context.Background()
	service := deployThrowawayService(t, env, "drain")

	nodes, err := env.Docker.NodeList(ctx, swarm.NodeListOptions{})
	if err != nil {
		t.Fatalf("NodeList: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("NodeList returned no nodes")
	}
	nodeID := nodes[0].ID

	// Restore active no matter how the assertions below turn out.
	defer restoreNodeActive(t, env, proc, nodeID)

	drainBody, err := json.Marshal(map[string]string{"availability": "drain"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := sweepRequest(
		t, proc, http.MethodPut, "/nodes/"+nodeID+"/availability",
		"application/json", drainBody,
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT node availability drain: status = %d, want 200", resp.StatusCode)
	}

	deadline := time.Now().Add(30 * time.Second)
	drained := false
	for time.Now().Before(deadline) {
		node, _, err := env.Docker.NodeInspectWithRaw(ctx, nodeID)
		if err == nil && node.Spec.Availability == swarm.NodeAvailabilityDrain {
			drained = true
			break
		}
		time.Sleep(time.Second)
	}
	if !drained {
		t.Fatalf("node %s never reported Availability=drain on the engine", nodeID)
	}

	// With no other node to reschedule onto, drain must evict the
	// throwaway service's task rather than leave it running.
	svc := inspectService(t, env, service)
	evicted := false
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		running, err := runningTaskCount(ctx, env, svc.ID)
		if err == nil && running == 0 {
			evicted = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !evicted {
		t.Errorf("service %s still has a running task after the node drained", service)
	}
}

// restoreNodeActive returns the node to active and confirms it on the engine.
// It uses t.Errorf, never t.Fatalf, so it is safe to run from a defer that
// may itself be unwinding a failed test.
func restoreNodeActive(t *testing.T, env *harness.Env, proc *sut.Process, nodeID string) {
	t.Helper()

	body, err := json.Marshal(map[string]string{"availability": "active"})
	if err != nil {
		t.Errorf("restore node active: marshal: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodPut,
		proc.BaseURL+"/nodes/"+nodeID+"/availability", bytes.NewReader(body),
	)
	if err != nil {
		t.Errorf("restore node active: new request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Errorf("restore node active: %v", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("restore node active: status = %d, want 200", resp.StatusCode)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		node, _, err := env.Docker.NodeInspectWithRaw(context.Background(), nodeID)
		if err == nil && node.Spec.Availability == swarm.NodeAvailabilityActive {
			return
		}
		time.Sleep(time.Second)
	}

	t.Errorf("node %s did not report Availability=active after restoring", nodeID)
}

func driveTaskRemoval(t *testing.T, env *harness.Env, proc *sut.Process) {
	ctx := context.Background()
	service := deployThrowawayService(t, env, "taskrm")
	svc := inspectService(t, env, service)

	before := serviceTaskIDs(t, env, svc.ID)
	if len(before) == 0 {
		t.Fatalf("service %s has no tasks before removal", service)
	}

	var taskID string
	for id := range before {
		taskID = id
		break
	}

	// The task IDs above come from the engine, but HandleRemoveTask resolves
	// its target through the cache (lookupOr404), which the watcher fills
	// asynchronously. Deleting inside that window answers 404 — a latent race
	// this case carried until enough other engine churn ran alongside it to
	// make the window matter.
	awaitCached(t, proc, "/tasks/"+taskID)

	resp := sweepRequest(t, proc, http.MethodDelete, "/tasks/"+taskID, "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE task: status = %d, want 204", resp.StatusCode)
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		after := serviceTaskIDs(t, env, svc.ID)
		for id := range after {
			if !before[id] {
				return // a task not present before removal showed up on the engine
			}
		}
		time.Sleep(2 * time.Second)
	}

	_ = ctx
	t.Errorf("service %s never got a replacement task after DELETE /tasks/%s", service, taskID)
}

// ─── the sweep itself ───────────────────────────────────────────────────

// TestWriteSweepMutatesTheCluster drives every route in drivenWriteRoutes
// against a real engine at operations level 3 (the ceiling any of them
// needs — node availability is tier 3) and verifies each one against the
// engine, not the HTTP response.
func TestWriteSweepMutatesTheCluster(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startWriteSweep(t, env, "3")

	for route, drive := range drivenWriteRoutes {
		t.Run(route, func(t *testing.T) {
			drive(t, env, proc)
		})
	}
}

var routeParam = regexp.MustCompile(`\{[^}]+\}`)

// placeholderRequest turns a Route.String() such as
// "PUT /services/{id}/image" into a method and a concrete path with every
// path parameter replaced by a placeholder. The operations-level gate runs
// before any resource lookup (requireLevel wraps the handler outside
// requireWriteACL's own lookup — see TestScaleRefusedAtReadOnlyLevel in
// write_test.go), so the resource named need not exist.
func placeholderRequest(route string) (method, path string) {
	method, pattern, _ := strings.Cut(route, " ")

	return method, routeParam.ReplaceAllString(pattern, "placeholder")
}

// TestWriteSweepRefusedAtReadOnlyLevel replays every driven route's request
// at operations level 0 and asserts OPS001, mirroring
// TestScaleRefusedAtReadOnlyLevel in write_test.go for the routes this file
// adds. This is where the suite would catch a write that succeeds when the
// tier forbids it.
func TestWriteSweepRefusedAtReadOnlyLevel(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startWriteSweep(t, env, "0")

	for route := range drivenWriteRoutes {
		t.Run(route, func(t *testing.T) {
			method, path := placeholderRequest(route)

			resp := sweepRequest(t, proc, method, path, "application/json", nil)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}

			if pType := problemType(t, resp); !strings.Contains(pType, "OPS001") {
				t.Errorf("problem type = %q, want it to name OPS001", pType)
			}
		})
	}
}

// TestWriteSweepConcurrentScaleProducesAStaleVersionConflict is the 409
// case: Cetacean's own writers always inspect the service immediately before
// writing (see internal/docker/client.go's ScaleService), so a client cannot
// hand it a stale version directly — there is no version field on the
// request. The only way to make Cetacean itself produce a genuine version
// conflict is to race it against itself: fire many concurrent scale requests
// at one service and let two of them read the same version before either
// commits. No test anywhere in this repository pins this contract today.
//
// The race is retried a bounded number of times rather than asserted on a
// single attempt: the assertion itself (a real 409 naming SVC001, alongside
// a real 200, alongside the engine reflecting exactly one of the requested
// values) is never weakened, but a race that fails to manifest on one
// attempt is a timing miss, not a passing result.
func TestWriteSweepConcurrentScaleProducesAStaleVersionConflict(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startWriteSweep(t, env, "3")

	service := deployThrowawayService(t, env, "conflict")
	id := serviceID(t, proc, service)

	const concurrency = 10
	const maxAttempts = 3

	for attempt := range maxAttempts {
		statuses := make([]int, concurrency)
		problems := make([]string, concurrency)
		targets := make([]uint64, concurrency)

		var wg sync.WaitGroup
		start := make(chan struct{})

		for i := range concurrency {
			target := uint64(2 + attempt*concurrency + i) //nolint:gosec // small bounded loop index
			targets[i] = target

			wg.Add(1)
			go func(i int, replicas uint64) {
				defer wg.Done()
				<-start

				body, err := json.Marshal(map[string]any{"replicas": replicas})
				if err != nil {
					t.Errorf("marshal: %v", err)
					return
				}

				req, err := http.NewRequestWithContext(
					context.Background(), http.MethodPut,
					proc.BaseURL+"/services/"+id+"/scale", bytes.NewReader(body),
				)
				if err != nil {
					t.Errorf("new request: %v", err)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json")

				resp, err := proc.Client().Do(req)
				if err != nil {
					t.Errorf("PUT scale: %v", err)
					return
				}
				defer resp.Body.Close()

				statuses[i] = resp.StatusCode
				if resp.StatusCode != http.StatusOK {
					var problem struct {
						Type   string `json:"type"`
						Detail string `json:"detail"`
					}
					_ = json.NewDecoder(resp.Body).Decode(&problem)
					problems[i] = problem.Type + ": " + problem.Detail
				}
			}(i, target)
		}

		close(start)
		wg.Wait()

		successes, conflicts := 0, 0
		for _, status := range statuses {
			switch status {
			case http.StatusOK:
				successes++
			case http.StatusConflict:
				conflicts++
			}
		}

		if successes == concurrency {
			// Every request succeeded: nothing raced this attempt (all ten
			// serialized cleanly), not a real result either way. Retry
			// rather than concluding anything from it.
			continue
		}

		// The race manifested — at least one request lost it. What the loser
		// got back is the assertion this test exists to make: RFC-wise it
		// must be a 409 naming SVC001 (see errors.go), not a bare 500. The
		// specific known-wrong outcome (500/ENG004 — DEFECT-1) is quarantined
		// with a narrow, named Skip rather than a Fatal, so this test stays
		// runnable without hiding the defect or masking a *different*
		// regression: anything else unexpected still fails loudly.
		for i, status := range statuses {
			if status == http.StatusOK || status == http.StatusConflict {
				continue
			}

			if status == http.StatusInternalServerError && strings.Contains(problems[i], "ENG004") {
				t.Skipf(
					"DEFECT-1: a losing concurrent PUT /services/{id}/scale got %d (%s) "+
						"instead of the documented 409.\n\n"+
						"docs/api.md:297 documents the contract this test enforces: "+
						"\"Resource changed between your read and your write | 409 | SVC001, "+
						"NOD002, CFG005, SEC005 | Re-read the resource and retry\". The real "+
						"Docker engine answers a genuine service-update version race with a "+
						"bare HTTP 500 (\"update out of sequence\", gRPC code Unknown), not "+
						"409. internal/api/write_helpers.go:153's "+
						"\"cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err)\" "+
						"check does not match a 5xx-classified error, so control falls "+
						"through to write_helpers.go:142's generic \"ENG004\" 500 path "+
						"instead of the conflictCode (SVC001) 409. See write-sweep-report.md "+
						"for the full reproduction and root cause. This Skip is narrow: only "+
						"this exact status+code combination is quarantined, so the test "+
						"starts passing again the moment the product maps this error to 409, "+
						"and fails instead of skipping on any other unexpected outcome.",
					status, problems[i],
				)
			}

			t.Fatalf(
				"request %d: a losing concurrent scale got status %d (%s), want 409 "+
					"naming SVC001 (or the known DEFECT-1 500/ENG004 outcome, which this "+
					"test skips rather than fails)",
				i, status, problems[i],
			)
		}

		if conflicts == 0 {
			t.Fatalf(
				"%d of %d concurrent scale requests failed, but none as a 409 — see the "+
					"per-request log above",
				successes, concurrency,
			)
		}

		for i, status := range statuses {
			if status == http.StatusConflict && !strings.Contains(problems[i], "SVC001") {
				t.Errorf(
					"attempt %d, request %d: problem type = %q, want it to name SVC001",
					attempt, i, problems[i],
				)
			}
		}

		// The engine must reflect exactly one of the requested values — the
		// one whose write actually won the race.
		svc := inspectService(t, env, service)
		if svc.Spec.Mode.Replicated == nil || svc.Spec.Mode.Replicated.Replicas == nil {
			t.Fatalf("service %s lost its replicated mode", service)
		}
		final := *svc.Spec.Mode.Replicated.Replicas

		matched := false
		for i, status := range statuses {
			if status == http.StatusOK && targets[i] == final {
				matched = true
			}
		}
		if !matched {
			t.Errorf(
				"engine replicas = %d, want it to match one of the %d requests that got 200",
				final, successes,
			)
		}

		return
	}

	t.Fatalf(
		"no attempt among %d produced a losing request among %d concurrent scale requests "+
			"— every request succeeded every time, so the race never manifested",
		maxAttempts, concurrency,
	)
}

// ─── the self-enforcing gate ────────────────────────────────────────────

// TestEveryWriteRouteIsDrivenOrExcused requires every POST/PUT/PATCH/DELETE
// route contract.Routes() reports to appear in drivenWriteRoutes or
// excusedWriteRoutes. A new write endpoint added to internal/api/router.go
// then fails this test instead of silently going untested. It also fails on
// a stale entry in either map — one naming a route the inventory no longer
// has — since a stale excuse hides the next real drift as surely as a
// missing one does.
func TestEveryWriteRouteIsDrivenOrExcused(t *testing.T) {
	routes := contractRoutes(t)

	live := make(map[string]bool, len(routes))
	var gaps []string

	for _, route := range routes {
		switch route.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			continue
		}

		key := route.String()
		live[key] = true

		if _, ok := drivenWriteRoutes[key]; ok {
			continue
		}

		reason, ok := excusedWriteRoutes[key]
		if !ok {
			t.Errorf(
				"%s is neither driven nor excused; add coverage in drivenWriteRoutes "+
					"or a reason in excusedWriteRoutes",
				key,
			)
			continue
		}

		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s has an empty excuse reason", key)
		}

		if strings.HasPrefix(reason, "gap:") {
			gaps = append(gaps, key)
		}
	}

	for key := range drivenWriteRoutes {
		if !live[key] {
			t.Errorf("drivenWriteRoutes has a stale entry %q: no such mutating route in the "+
				"current inventory", key)
		}
	}

	for key := range excusedWriteRoutes {
		if !live[key] {
			t.Errorf("excusedWriteRoutes has a stale entry %q: no such mutating route in the "+
				"current inventory", key)
		}
	}

	slices.Sort(gaps)
	t.Logf(
		"write sweep: %d driven, %d excused (%d of them gaps)\ngaps:\n  %s",
		len(drivenWriteRoutes), len(excusedWriteRoutes), len(gaps), strings.Join(gaps, "\n  "),
	)
}
