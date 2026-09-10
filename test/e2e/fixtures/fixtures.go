//go:build e2e

// Package fixtures populates the end-to-end swarm. The baseline cluster is
// shared by every read-only case; mutation cases deploy their own stacks.
package fixtures

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/test/e2e/harness"
)

const (
	StackShop     = "shop"
	StackPlatform = "platform"

	CrashLoopService = "shop_flaky"
	OrphanConfig     = "orphan-config"
	OrphanSecret     = "orphan-secret"
	OrphanNetwork    = "orphan-net"
	OrphanVolume     = "orphan-vol"

	fixtureImage       = "cetacean-e2e-fixture:latest"
	imageBuildTimeout  = 5 * time.Minute
	convergeTimeout    = 3 * time.Minute
	convergePollEvery  = 2 * time.Second
	removeStackTimeout = 30 * time.Second

	stackLabel = "com.docker.stack.namespace"
)

// ServiceSpec is the subset of a service definition the fixtures need.
type ServiceSpec struct {
	Name     string
	Replicas uint64
	Global   bool
	Command  []string
	Labels   map[string]string
}

// DeployBaseline deploys the shared cluster. It is idempotent so a second
// caller in the same run is free.
func DeployBaseline(t *testing.T, env *harness.Env) {
	t.Helper()

	ensureImage(t, env)

	if baselinePresent(t, env) {
		return
	}

	createNetwork(t, env, "shop-net", map[string]string{stackLabel: StackShop})
	createNetwork(t, env, OrphanNetwork, nil)
	createVolume(t, env, OrphanVolume, nil)
	createConfig(
		t,
		env,
		"shop-config",
		[]byte("greeting=hello\n"),
		map[string]string{stackLabel: StackShop},
	)
	createConfig(t, env, OrphanConfig, []byte("unused\n"), nil)
	createSecret(
		t,
		env,
		"shop-secret",
		[]byte("s3cr3t\n"),
		map[string]string{stackLabel: StackShop},
	)
	createSecret(t, env, OrphanSecret, []byte("unused\n"), nil)

	specs := []ServiceSpec{
		{
			Name:     "shop_web",
			Replicas: 2,
			Command:  []string{"sleep infinity"},
			Labels: map[string]string{
				stackLabel:                      StackShop,
				"traefik.enable":                "true",
				"traefik.http.routers.web.rule": "Host(`shop.local`)",
			},
		},
		{
			Name:     "shop_lonely",
			Replicas: 1,
			Command:  []string{"sleep infinity"},
			Labels:   map[string]string{stackLabel: StackShop},
		},
		{
			// Exits immediately, so failed tasks and the restart counter have
			// real input rather than a simulated one.
			Name:     CrashLoopService,
			Replicas: 1,
			Command:  []string{"exit 1"},
			Labels:   map[string]string{stackLabel: StackShop},
		},
		{
			Name:    "platform_agent",
			Global:  true,
			Command: []string{"sleep infinity"},
			Labels: map[string]string{
				stackLabel:               StackPlatform,
				"shepherd.enable":        "true",
				"diun.enable":            "true",
				"swarm.cronjob.enable":   "true",
				"swarm.cronjob.schedule": "0 3 * * *",
			},
		},
	}

	for _, spec := range specs {
		createService(t, env, spec)
	}

	// The crash-looping service never converges by design; wait on the rest.
	for _, spec := range specs {
		if spec.Name == CrashLoopService {
			continue
		}

		waitConverged(t, env, spec.Name)
	}
}

// DeployStack deploys a throwaway stack and removes it in cleanup.
func DeployStack(t *testing.T, env *harness.Env, name string, specs []ServiceSpec) string {
	t.Helper()

	ensureImage(t, env)

	stack := fmt.Sprintf("%s-%d", name, time.Now().UnixNano())

	for _, spec := range specs {
		spec.Name = stack + "_" + spec.Name

		if spec.Labels == nil {
			spec.Labels = map[string]string{}
		}

		spec.Labels[stackLabel] = stack

		createService(t, env, spec)
	}

	t.Cleanup(func() { removeStack(t, env, stack) })

	for _, spec := range specs {
		waitConverged(t, env, stack+"_"+spec.Name)
	}

	return stack
}

// ensureImage builds the fixture image on the host and loads it into the DinD
// engine. Pulling inside DinD on every run would make the suite slow and
// network-dependent.
func ensureImage(t *testing.T, env *harness.Env) {
	t.Helper()

	images, err := env.Docker.ImageList(t.Context(), image.ListOptions{})
	if err != nil {
		t.Fatalf("ImageList: %v", err)
	}

	for _, img := range images {
		if slices.Contains(img.RepoTags, fixtureImage) {
			return
		}
	}

	root := repoRoot(t)

	buildCtx, cancel := context.WithTimeout(t.Context(), imageBuildTimeout)
	defer cancel()

	// fixtureImage is a package constant and root resolves from the source
	// tree, not from user input.
	build := exec.CommandContext( //nolint:gosec // G204: fixed args, no user input
		buildCtx,
		"docker",
		"build",
		"-t",
		fixtureImage,
		root+"/test/e2e/fixtures/image",
	)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixture image: %v\n%s", err, out)
	}

	saveCtx, cancel := context.WithTimeout(t.Context(), imageBuildTimeout)
	defer cancel()

	save := exec.CommandContext(saveCtx, "docker", "save", fixtureImage)

	var tar bytes.Buffer
	save.Stdout = &tar

	if err := save.Run(); err != nil {
		t.Fatalf("save fixture image: %v", err)
	}

	resp, err := env.Docker.ImageLoad(t.Context(), &tar, client.ImageLoadWithQuiet(true))
	if err != nil {
		t.Fatalf("ImageLoad: %v", err)
	}
	defer resp.Body.Close()

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("drain ImageLoad: %v", err)
	}
}

// baselinePresent reports whether the shared shop stack has already been
// deployed, which is what makes DeployBaseline idempotent.
func baselinePresent(t *testing.T, env *harness.Env) bool {
	t.Helper()

	services, err := env.Docker.ServiceList(t.Context(), swarm.ServiceListOptions{})
	if err != nil {
		t.Fatalf("ServiceList: %v", err)
	}

	for _, svc := range services {
		if svc.Spec.Labels[stackLabel] == StackShop {
			return true
		}
	}

	return false
}

func createNetwork(t *testing.T, env *harness.Env, name string, labels map[string]string) {
	t.Helper()

	_, err := env.Docker.NetworkCreate(t.Context(), name, network.CreateOptions{
		Driver:     "overlay",
		Attachable: true,
		Labels:     labels,
	})
	if err != nil && !cerrdefs.IsConflict(err) {
		t.Fatalf("NetworkCreate %s: %v", name, err)
	}
}

func createVolume(t *testing.T, env *harness.Env, name string, labels map[string]string) {
	t.Helper()

	if _, err := env.Docker.VolumeCreate(t.Context(), volume.CreateOptions{
		Name:   name,
		Labels: labels,
	}); err != nil {
		t.Fatalf("VolumeCreate %s: %v", name, err)
	}
}

func createConfig(
	t *testing.T,
	env *harness.Env,
	name string,
	data []byte,
	labels map[string]string,
) {
	t.Helper()

	_, err := env.Docker.ConfigCreate(t.Context(), swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
		Data:        data,
	})
	if err != nil && !cerrdefs.IsConflict(err) {
		t.Fatalf("ConfigCreate %s: %v", name, err)
	}
}

func createSecret(
	t *testing.T,
	env *harness.Env,
	name string,
	data []byte,
	labels map[string]string,
) {
	t.Helper()

	_, err := env.Docker.SecretCreate(t.Context(), swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
		Data:        data,
	})
	if err != nil && !cerrdefs.IsConflict(err) {
		t.Fatalf("SecretCreate %s: %v", name, err)
	}
}

func createService(t *testing.T, env *harness.Env, spec ServiceSpec) {
	t.Helper()

	mode := swarm.ServiceMode{}
	if spec.Global {
		mode.Global = &swarm.GlobalService{}
	} else {
		replicas := spec.Replicas
		mode.Replicated = &swarm.ReplicatedService{Replicas: &replicas}
	}

	_, err := env.Docker.ServiceCreate(t.Context(), swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: spec.Name, Labels: spec.Labels},
		Mode:        mode,
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:   fixtureImage,
				Command: []string{"/bin/sh", "-c"},
				Args:    spec.Command,
			},
			RestartPolicy: &swarm.RestartPolicy{
				Condition: swarm.RestartPolicyConditionAny,
				Delay:     new(2 * time.Second),
			},
		},
	}, swarm.ServiceCreateOptions{})
	if err != nil && !cerrdefs.IsConflict(err) {
		t.Fatalf("ServiceCreate %s: %v", spec.Name, err)
	}
}

// waitConverged blocks until the service settles, using the product's own
// rule so the harness and Cetacean cannot disagree about what settled means.
func waitConverged(t *testing.T, env *harness.Env, name string) {
	t.Helper()

	deadline := time.Now().Add(convergeTimeout)
	last := "no observation yet"

	for time.Now().Before(deadline) {
		svc, _, err := env.Docker.ServiceInspectWithRaw(
			t.Context(),
			name,
			swarm.ServiceInspectOptions{},
		)
		if err != nil {
			last = err.Error()
			time.Sleep(convergePollEvery)

			continue
		}

		running := runningTasks(t, env, svc.ID)

		converged, msg := cluster.ServiceConverged(svc, running)
		last = msg

		if converged {
			return
		}

		time.Sleep(convergePollEvery)
	}

	t.Fatalf("service %s did not converge within %s: %s", name, convergeTimeout, last)
}

func runningTasks(t *testing.T, env *harness.Env, serviceID string) int {
	t.Helper()

	tasks, err := env.Docker.TaskList(t.Context(), swarm.TaskListOptions{})
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}

	count := 0
	for _, task := range tasks {
		if task.ServiceID == serviceID && task.Status.State == swarm.TaskStateRunning {
			count++
		}
	}

	return count
}

// removeStack removes every service and network carrying the stack's label.
// It runs from t.Cleanup, where t.Context() is already canceled, so it uses
// an independent, bounded context rather than the test's own.
func removeStack(t *testing.T, env *harness.Env, stack string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), removeStackTimeout)
	defer cancel()

	stackFilter := filters.NewArgs(filters.Arg("label", stackLabel+"="+stack))

	services, err := env.Docker.ServiceList(ctx, swarm.ServiceListOptions{Filters: stackFilter})
	if err != nil {
		t.Logf("ServiceList for stack %s: %v", stack, err)
	}

	for _, svc := range services {
		if err := env.Docker.ServiceRemove(ctx, svc.ID); err != nil {
			t.Logf("ServiceRemove %s: %v", svc.Spec.Name, err)
		}
	}

	networks, err := env.Docker.NetworkList(ctx, network.ListOptions{Filters: stackFilter})
	if err != nil {
		t.Logf("NetworkList for stack %s: %v", stack, err)

		return
	}

	for _, net := range networks {
		if err := env.Docker.NetworkRemove(ctx, net.ID); err != nil {
			t.Logf("NetworkRemove %s: %v", net.Name, err)
		}
	}
}

// repoRoot walks up from this source file to the module root, mirroring
// harness.go's own resolution — this package sits at the same depth.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("cannot locate fixtures source")
	}

	// .../test/e2e/fixtures/fixtures.go -> repo root is three levels up.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
