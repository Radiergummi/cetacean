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
	"strings"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
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
	UsedVolume       = "shop-data"

	fixtureImage         = "cetacean-e2e-fixture:latest"
	imageBuildTimeout    = 5 * time.Minute
	convergeTimeout      = 3 * time.Minute
	convergePollEvery    = 2 * time.Second
	removeStackTimeout   = 60 * time.Second
	removeStackPollEvery = 500 * time.Millisecond

	stackLabel = "com.docker.stack.namespace"

	// baselineSentinel is a config created as the very last step of
	// DeployBaseline. baselinePresent tests only for this, not for any
	// individual resource, so a run that fails partway through leaves no
	// sentinel and the next call re-drives the whole baseline rather than
	// silently adopting a half-built one.
	baselineSentinel = "e2e-baseline-complete"
)

// ServiceSpec is the subset of a service definition the fixtures need.
type ServiceSpec struct {
	Name     string
	Replicas uint64
	Global   bool
	Command  []string
	Labels   map[string]string
	Configs  []string    // config names to mount
	Secrets  []string    // secret names to mount
	Networks []string    // network names to attach
	Mounts   []MountSpec // volumes to mount
}

// MountSpec attaches a named volume at a path in the container.
type MountSpec struct {
	Volume string
	Target string
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
	createVolume(t, env, UsedVolume, map[string]string{stackLabel: StackShop})
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
			Configs:  []string{"shop-config"},
			Secrets:  []string{"shop-secret"},
			Networks: []string{"shop-net"},
			Mounts:   []MountSpec{{Volume: UsedVolume, Target: "/data"}},
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

	// Last step, deliberately: its presence is what DeployBaseline's
	// idempotency check relies on, so a run that failed earlier leaves no
	// sentinel and gets re-driven rather than adopted half-built.
	createConfig(t, env, baselineSentinel, []byte("ok\n"), nil)
}

// DeployStack deploys a throwaway stack and removes it in cleanup.
func DeployStack(t *testing.T, env *harness.Env, name string, specs []ServiceSpec) string {
	t.Helper()

	ensureImage(t, env)

	stack := fmt.Sprintf("%s-%d", name, time.Now().UnixNano())

	// Registered before anything is created: removeStack already tolerates a
	// stack with no services, so if a create call below fails partway
	// through, cleanup still runs rather than leaking what succeeded.
	t.Cleanup(func() { removeStack(t, env, stack) })

	for _, spec := range specs {
		spec.Name = stack + "_" + spec.Name

		if spec.Labels == nil {
			spec.Labels = map[string]string{}
		}

		spec.Labels[stackLabel] = stack

		createService(t, env, spec)
	}

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

// baselinePresent reports whether a previous DeployBaseline call ran to
// completion. It tests only for baselineSentinel — not for any individual
// resource — so a run that failed partway through (leaving no sentinel) is
// re-driven rather than mistaken for a finished baseline.
func baselinePresent(t *testing.T, env *harness.Env) bool {
	t.Helper()

	configs, err := env.Docker.ConfigList(t.Context(), swarm.ConfigListOptions{
		Filters: filters.NewArgs(filters.Arg("name", baselineSentinel)),
	})
	if err != nil {
		t.Fatalf("ConfigList: %v", err)
	}

	for _, cfg := range configs {
		if cfg.Spec.Name == baselineSentinel {
			return true
		}
	}

	return false
}

// isConflict tolerates a resource that already exists. cerrdefs.IsConflict
// covers the Docker daemon's own conflict response, but the swarm raft
// allocator raises a differently-shaped error for the same situation ("rpc
// error: code = Unknown desc = name conflicts with an existing object") when
// two callers race to create the same object -- e.g. two test packages
// running DeployBaseline's check-then-act sentinel guard concurrently,
// without -p 1. Recognizing both keeps that race a harness constraint rather
// than a false failure.
func isConflict(err error) bool {
	return cerrdefs.IsConflict(err) ||
		strings.Contains(err.Error(), "name conflicts with an existing object")
}

func createNetwork(t *testing.T, env *harness.Env, name string, labels map[string]string) {
	t.Helper()

	_, err := env.Docker.NetworkCreate(t.Context(), name, network.CreateOptions{
		Driver:     "overlay",
		Attachable: true,
		Labels:     labels,
	})
	if err != nil && !isConflict(err) {
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
	if err != nil && !isConflict(err) {
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
	if err != nil && !isConflict(err) {
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

	configRefs := make([]*swarm.ConfigReference, len(spec.Configs))
	for i, name := range spec.Configs {
		configRefs[i] = &swarm.ConfigReference{
			ConfigID:   configID(t, env, name),
			ConfigName: name,
			File: &swarm.ConfigReferenceFileTarget{
				Name: name,
				UID:  "0",
				GID:  "0",
				Mode: 0o444,
			},
		}
	}

	secretRefs := make([]*swarm.SecretReference, len(spec.Secrets))
	for i, name := range spec.Secrets {
		secretRefs[i] = &swarm.SecretReference{
			SecretID:   secretID(t, env, name),
			SecretName: name,
			File: &swarm.SecretReferenceFileTarget{
				Name: name,
				UID:  "0",
				GID:  "0",
				Mode: 0o400,
			},
		}
	}

	mounts := make([]mount.Mount, len(spec.Mounts))
	for i, m := range spec.Mounts {
		mounts[i] = mount.Mount{Type: mount.TypeVolume, Source: m.Volume, Target: m.Target}
	}

	networks := make([]swarm.NetworkAttachmentConfig, len(spec.Networks))
	for i, name := range spec.Networks {
		networks[i] = swarm.NetworkAttachmentConfig{Target: name}
	}

	_, err := env.Docker.ServiceCreate(t.Context(), swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: spec.Name, Labels: spec.Labels},
		Mode:        mode,
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:   fixtureImage,
				Command: []string{"/bin/sh", "-c"},
				Args:    spec.Command,
				Configs: configRefs,
				Secrets: secretRefs,
				Mounts:  mounts,
			},
			Networks: networks,
			RestartPolicy: &swarm.RestartPolicy{
				Condition: swarm.RestartPolicyConditionAny,
				Delay:     new(2 * time.Second),
			},
		},
	}, swarm.ServiceCreateOptions{})
	if err != nil && !isConflict(err) {
		t.Fatalf("ServiceCreate %s: %v", spec.Name, err)
	}
}

// configID resolves a config's name to the ID the SDK requires for a
// ContainerSpec reference; the daemon rejects a reference carrying only a
// name.
func configID(t *testing.T, env *harness.Env, name string) string {
	t.Helper()

	configs, err := env.Docker.ConfigList(t.Context(), swarm.ConfigListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		t.Fatalf("ConfigList %s: %v", name, err)
	}

	for _, cfg := range configs {
		if cfg.Spec.Name == name {
			return cfg.ID
		}
	}

	t.Fatalf("config %s not found", name)

	return ""
}

// secretID is configID's counterpart for secrets.
func secretID(t *testing.T, env *harness.Env, name string) string {
	t.Helper()

	secrets, err := env.Docker.SecretList(t.Context(), swarm.SecretListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		t.Fatalf("SecretList %s: %v", name, err)
	}

	for _, sec := range secrets {
		if sec.Spec.Name == name {
			return sec.ID
		}
	}

	t.Fatalf("secret %s not found", name)

	return ""
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
//
// Both stages have to wait, not just fire once: ServiceRemove and the task
// teardown it triggers are asynchronous, so a network can still show "active
// endpoints" for a moment after its services are gone. removeStack first
// polls ServiceList to confirm the stack's services have actually left the
// engine (not just that ServiceRemove was accepted), then retries each
// NetworkRemove with a bounded backoff to ride out the remaining endpoint-
// detachment lag. A failure that survives the deadline is reported with
// t.Errorf, not logged: a leaked network collides with the next run's
// same-named create, and a t.Logf nobody reads is how that leak went
// unnoticed before.
func removeStack(t *testing.T, env *harness.Env, stack string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), removeStackTimeout)
	defer cancel()

	deadline := time.Now().Add(removeStackTimeout)
	stackFilter := filters.NewArgs(filters.Arg("label", stackLabel+"="+stack))

	services, err := env.Docker.ServiceList(ctx, swarm.ServiceListOptions{Filters: stackFilter})
	if err != nil {
		t.Errorf("ServiceList for stack %s: %v", stack, err)
	}

	for _, svc := range services {
		if err := env.Docker.ServiceRemove(ctx, svc.ID); err != nil {
			t.Errorf("ServiceRemove %s: %v", svc.Spec.Name, err)
		}
	}

	if len(services) > 0 {
		waitServicesGone(t, ctx, env, stackFilter, stack, deadline)
	}

	networks, err := env.Docker.NetworkList(ctx, network.ListOptions{Filters: stackFilter})
	if err != nil {
		t.Errorf("NetworkList for stack %s: %v", stack, err)

		return
	}

	for _, net := range networks {
		removeNetworkWithRetry(t, ctx, env, net.ID, net.Name, deadline)
	}
}

// waitServicesGone blocks until the engine reports no services left under
// stackFilter, or deadline passes. ServiceRemove accepting the call does not
// mean the service — and the tasks and endpoints it owns — are actually torn
// down yet.
func waitServicesGone(
	t *testing.T,
	ctx context.Context,
	env *harness.Env,
	stackFilter filters.Args,
	stack string,
	deadline time.Time,
) {
	t.Helper()

	for time.Now().Before(deadline) {
		remaining, err := env.Docker.ServiceList(
			ctx,
			swarm.ServiceListOptions{Filters: stackFilter},
		)
		if err != nil {
			t.Errorf("ServiceList for stack %s: %v", stack, err)

			return
		}

		if len(remaining) == 0 {
			return
		}

		time.Sleep(removeStackPollEvery)
	}

	t.Errorf("stack %s: services still present after %s", stack, removeStackTimeout)
}

// removeNetworkWithRetry retries NetworkRemove until it succeeds or deadline
// passes, to ride out the endpoint-detachment lag that follows service
// removal. A network still standing when the deadline expires is reported
// against the test that leaked it, since it will collide with the next run's
// attempt to create a network of the same name.
func removeNetworkWithRetry(
	t *testing.T,
	ctx context.Context,
	env *harness.Env,
	id, name string,
	deadline time.Time,
) {
	t.Helper()

	var lastErr error

	for time.Now().Before(deadline) {
		lastErr = env.Docker.NetworkRemove(ctx, id)
		if lastErr == nil {
			return
		}

		time.Sleep(removeStackPollEvery)
	}

	t.Errorf("NetworkRemove %s: %v (network left behind for the next run)", name, lastErr)
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
