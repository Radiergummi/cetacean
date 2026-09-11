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

	// removeNetworkTimeout is the network stage's own budget. It cannot share
	// removeStackTimeout with the service stage: service teardown consuming
	// the whole budget is exactly the slow case this cleanup exists for, and
	// a shared deadline then leaves nothing for the removals that follow.
	removeNetworkTimeout = 60 * time.Second

	stackLabel = "com.docker.stack.namespace"

	// BaselineSentinel is a config created as the very last step of
	// DeployBaseline. baselinePresent tests only for this, not for any
	// individual resource, so a run that fails partway through leaves no
	// sentinel and the next call re-drives the whole baseline rather than
	// silently adopting a half-built one.
	BaselineSentinel = "e2e-baseline-complete"
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

	if err := DeployBaselineCLI(env); err != nil {
		t.Fatalf("%v", err)
	}
}

// DeployBaselineCLI is DeployBaseline's non-test entry point, for callers
// that have no *testing.T — e.g. cmd/e2eenv. It returns the first error
// rather than failing a test, so it shares the same deployment logic instead
// of a second copy of it.
func DeployBaselineCLI(env *harness.Env) error {
	ctx := context.Background()

	if err := ensureImage(ctx, env); err != nil {
		return err
	}

	present, err := baselinePresent(ctx, env)
	if err != nil {
		return err
	}

	if present {
		return nil
	}

	if err := createNetwork(
		ctx,
		env,
		"shop-net",
		map[string]string{stackLabel: StackShop},
	); err != nil {
		return err
	}

	if err := createNetwork(ctx, env, OrphanNetwork, nil); err != nil {
		return err
	}

	if err := createVolume(
		ctx,
		env,
		UsedVolume,
		map[string]string{stackLabel: StackShop},
	); err != nil {
		return err
	}

	if err := createVolume(ctx, env, OrphanVolume, nil); err != nil {
		return err
	}

	if err := createConfig(
		ctx,
		env,
		"shop-config",
		[]byte("greeting=hello\n"),
		map[string]string{stackLabel: StackShop},
	); err != nil {
		return err
	}

	if err := createConfig(ctx, env, OrphanConfig, []byte("unused\n"), nil); err != nil {
		return err
	}

	if err := createSecret(
		ctx,
		env,
		"shop-secret",
		[]byte("s3cr3t\n"),
		map[string]string{stackLabel: StackShop},
	); err != nil {
		return err
	}

	if err := createSecret(ctx, env, OrphanSecret, []byte("unused\n"), nil); err != nil {
		return err
	}

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
		if err := createService(ctx, env, spec); err != nil {
			return err
		}
	}

	// The crash-looping service never converges by design; wait on the rest.
	for _, spec := range specs {
		if spec.Name == CrashLoopService {
			continue
		}

		if err := waitConverged(ctx, env, spec.Name); err != nil {
			return err
		}
	}

	// Last step, deliberately: its presence is what DeployBaseline's
	// idempotency check relies on, so a run that failed earlier leaves no
	// sentinel and gets re-driven rather than adopted half-built.
	return createConfig(ctx, env, BaselineSentinel, []byte("ok\n"), nil)
}

// DeployStack deploys a throwaway stack and removes it in cleanup.
func DeployStack(t *testing.T, env *harness.Env, name string, specs []ServiceSpec) string {
	t.Helper()

	ctx := t.Context()

	if err := ensureImage(ctx, env); err != nil {
		t.Fatalf("%v", err)
	}

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

		if err := createService(ctx, env, spec); err != nil {
			t.Fatalf("%v", err)
		}
	}

	for _, spec := range specs {
		if err := waitConverged(ctx, env, stack+"_"+spec.Name); err != nil {
			t.Fatalf("%v", err)
		}
	}

	return stack
}

// ensureImage builds the fixture image on the host and loads it into the DinD
// engine. Pulling inside DinD on every run would make the suite slow and
// network-dependent.
func ensureImage(ctx context.Context, env *harness.Env) error {
	images, err := env.Docker.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return fmt.Errorf("ImageList: %w", err)
	}

	for _, img := range images {
		if slices.Contains(img.RepoTags, fixtureImage) {
			return nil
		}
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	buildCtx, cancel := context.WithTimeout(ctx, imageBuildTimeout)
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
		return fmt.Errorf("build fixture image: %w\n%s", err, out)
	}

	saveCtx, cancel := context.WithTimeout(ctx, imageBuildTimeout)
	defer cancel()

	save := exec.CommandContext(saveCtx, "docker", "save", fixtureImage)

	var tar bytes.Buffer
	save.Stdout = &tar

	if err := save.Run(); err != nil {
		return fmt.Errorf("save fixture image: %w", err)
	}

	resp, err := env.Docker.ImageLoad(ctx, &tar, client.ImageLoadWithQuiet(true))
	if err != nil {
		return fmt.Errorf("ImageLoad: %w", err)
	}
	defer resp.Body.Close()

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("drain ImageLoad: %w", err)
	}

	return nil
}

// baselinePresent reports whether a previous DeployBaseline call ran to
// completion. It tests only for BaselineSentinel — not for any individual
// resource — so a run that failed partway through (leaving no sentinel) is
// re-driven rather than mistaken for a finished baseline.
func baselinePresent(ctx context.Context, env *harness.Env) (bool, error) {
	configs, err := env.Docker.ConfigList(ctx, swarm.ConfigListOptions{
		Filters: filters.NewArgs(filters.Arg("name", BaselineSentinel)),
	})
	if err != nil {
		return false, fmt.Errorf("ConfigList: %w", err)
	}

	for _, cfg := range configs {
		if cfg.Spec.Name == BaselineSentinel {
			return true, nil
		}
	}

	return false, nil
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

func createNetwork(
	ctx context.Context,
	env *harness.Env,
	name string,
	labels map[string]string,
) error {
	_, err := env.Docker.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:     "overlay",
		Attachable: true,
		Labels:     labels,
	})
	if err != nil && !isConflict(err) {
		return fmt.Errorf("NetworkCreate %s: %w", name, err)
	}

	return nil
}

func createVolume(
	ctx context.Context,
	env *harness.Env,
	name string,
	labels map[string]string,
) error {
	// Tolerating a conflict matches the four sibling create helpers: a
	// re-drive over an existing baseline must converge, not fail.
	if _, err := env.Docker.VolumeCreate(ctx, volume.CreateOptions{
		Name:   name,
		Labels: labels,
	}); err != nil && !isConflict(err) {
		return fmt.Errorf("VolumeCreate %s: %w", name, err)
	}

	return nil
}

func createConfig(
	ctx context.Context,
	env *harness.Env,
	name string,
	data []byte,
	labels map[string]string,
) error {
	_, err := env.Docker.ConfigCreate(ctx, swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
		Data:        data,
	})
	if err != nil && !isConflict(err) {
		return fmt.Errorf("ConfigCreate %s: %w", name, err)
	}

	return nil
}

func createSecret(
	ctx context.Context,
	env *harness.Env,
	name string,
	data []byte,
	labels map[string]string,
) error {
	_, err := env.Docker.SecretCreate(ctx, swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
		Data:        data,
	})
	if err != nil && !isConflict(err) {
		return fmt.Errorf("SecretCreate %s: %w", name, err)
	}

	return nil
}

func createService(ctx context.Context, env *harness.Env, spec ServiceSpec) error {
	mode := swarm.ServiceMode{}
	if spec.Global {
		mode.Global = &swarm.GlobalService{}
	} else {
		replicas := spec.Replicas
		mode.Replicated = &swarm.ReplicatedService{Replicas: &replicas}
	}

	configRefs := make([]*swarm.ConfigReference, len(spec.Configs))

	for i, name := range spec.Configs {
		id, err := configID(ctx, env, name)
		if err != nil {
			return err
		}

		configRefs[i] = &swarm.ConfigReference{
			ConfigID:   id,
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
		id, err := secretID(ctx, env, name)
		if err != nil {
			return err
		}

		secretRefs[i] = &swarm.SecretReference{
			SecretID:   id,
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

	_, err := env.Docker.ServiceCreate(ctx, swarm.ServiceSpec{
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
		return fmt.Errorf("ServiceCreate %s: %w", spec.Name, err)
	}

	return nil
}

// configID resolves a config's name to the ID the SDK requires for a
// ContainerSpec reference; the daemon rejects a reference carrying only a
// name.
func configID(ctx context.Context, env *harness.Env, name string) (string, error) {
	configs, err := env.Docker.ConfigList(ctx, swarm.ConfigListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return "", fmt.Errorf("ConfigList %s: %w", name, err)
	}

	for _, cfg := range configs {
		if cfg.Spec.Name == name {
			return cfg.ID, nil
		}
	}

	return "", fmt.Errorf("config %s not found", name)
}

// secretID is configID's counterpart for secrets.
func secretID(ctx context.Context, env *harness.Env, name string) (string, error) {
	secrets, err := env.Docker.SecretList(ctx, swarm.SecretListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return "", fmt.Errorf("SecretList %s: %w", name, err)
	}

	for _, sec := range secrets {
		if sec.Spec.Name == name {
			return sec.ID, nil
		}
	}

	return "", fmt.Errorf("secret %s not found", name)
}

// waitConverged blocks until the service settles, using the product's own
// rule so the harness and Cetacean cannot disagree about what settled means.
func waitConverged(ctx context.Context, env *harness.Env, name string) error {
	deadline := time.Now().Add(convergeTimeout)
	last := "no observation yet"

	for time.Now().Before(deadline) {
		svc, _, err := env.Docker.ServiceInspectWithRaw(
			ctx,
			name,
			swarm.ServiceInspectOptions{},
		)
		if err != nil {
			last = err.Error()
			time.Sleep(convergePollEvery)

			continue
		}

		running, err := runningTasks(ctx, env, svc.ID)
		if err != nil {
			return err
		}

		converged, msg := cluster.ServiceConverged(svc, running)
		last = msg

		if converged {
			return nil
		}

		time.Sleep(convergePollEvery)
	}

	return fmt.Errorf("service %s did not converge within %s: %s", name, convergeTimeout, last)
}

func runningTasks(ctx context.Context, env *harness.Env, serviceID string) (int, error) {
	tasks, err := env.Docker.TaskList(ctx, swarm.TaskListOptions{})
	if err != nil {
		return 0, fmt.Errorf("TaskList: %w", err)
	}

	count := 0
	for _, task := range tasks {
		if task.ServiceID == serviceID && task.Status.State == swarm.TaskStateRunning {
			count++
		}
	}

	return count, nil
}

// removeStack removes every service and network carrying the stack's label.
// It runs from t.Cleanup, where t.Context() is already canceled, so it uses
// independent, bounded contexts rather than the test's own — one per stage,
// so a slow service teardown cannot leave the network stage with an expired
// context and no time to run in.
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

	netCtx, netCancel := context.WithTimeout(context.Background(), removeNetworkTimeout)
	defer netCancel()

	netDeadline := time.Now().Add(removeNetworkTimeout)

	networks, err := env.Docker.NetworkList(netCtx, network.ListOptions{Filters: stackFilter})
	if err != nil {
		t.Errorf("NetworkList for stack %s: %v", stack, err)

		return
	}

	for _, net := range networks {
		removeNetworkWithRetry(t, netCtx, env, net.ID, net.Name, netDeadline)
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
//
// It attempts the removal before consulting the deadline, so the report names
// a real failure: a deadline already spent on an earlier stage used to skip
// the loop body entirely, leaving lastErr nil and announcing a leaked network
// with a <nil> cause for a removal that was never tried.
func removeNetworkWithRetry(
	t *testing.T,
	ctx context.Context,
	env *harness.Env,
	id, name string,
	deadline time.Time,
) {
	t.Helper()

	var lastErr error

	for {
		lastErr = env.Docker.NetworkRemove(ctx, id)
		if lastErr == nil {
			return
		}

		if !time.Now().Add(removeStackPollEvery).Before(deadline) {
			break
		}

		time.Sleep(removeStackPollEvery)
	}

	t.Errorf("NetworkRemove %s: %v (network left behind for the next run)", name, lastErr)
}

// repoRoot walks up from this source file to the module root, mirroring
// harness.go's own resolution — this package sits at the same depth.
func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate fixtures source")
	}

	// .../test/e2e/fixtures/fixtures.go -> repo root is three levels up.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), nil
}
