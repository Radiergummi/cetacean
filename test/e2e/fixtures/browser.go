//go:build e2e

package fixtures

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/test/e2e/harness"
)

// Everything here belongs to the environment `make e2e-up` hands the browser
// suite, and to nothing else: the Go lanes assert exact shapes over the
// baseline, so DeployBaseline calls RemoveBrowserExtrasCLI first.

const (
	// Sorts last, so the first row the detail specs click is still a real
	// fixture.
	pageFillerPrefix = "zz-page-filler-"

	// Exceeds the dashboard's page size (pageSize in api/client.ts, 50).
	pageFillerCount = 60

	touchLabel = "cetacean.e2e.touched"
)

// DeployBrowserExtrasCLI adds what the browser suite needs beyond the
// baseline. It is idempotent.
func DeployBrowserExtrasCLI(env *harness.Env) error {
	ctx := context.Background()

	for i := range pageFillerCount {
		name := pageFillerPrefix + fmt.Sprintf("%03d", i)

		if err := createConfig(ctx, env, name, []byte("filler\n"), nil); err != nil {
			return err
		}
	}

	return nil
}

// RemoveBrowserExtrasCLI drops what DeployBrowserExtrasCLI created, so a Go
// lane adopting an `e2e-up` environment sees the baseline it expects.
func RemoveBrowserExtrasCLI(env *harness.Env) error {
	ctx := context.Background()

	configs, err := env.Docker.ConfigList(ctx, swarm.ConfigListOptions{})
	if err != nil {
		return fmt.Errorf("ConfigList: %w", err)
	}

	for _, cfg := range configs {
		if !strings.HasPrefix(cfg.Spec.Name, pageFillerPrefix) {
			continue
		}

		if err := env.Docker.ConfigRemove(ctx, cfg.ID); err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("ConfigRemove %s: %w", cfg.Spec.Name, err)
		}
	}

	return nil
}

// TouchForHistoryCLI relabels every baseline config, secret, service and node so
// the watcher records a change for each. It must run against an already running
// SUT: history is that process's ring buffer, and the initial sync records
// none. The label value differs per call, so a re-run still changes something.
func TouchForHistoryCLI(env *harness.Env) error {
	ctx := context.Background()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	if err := touchConfigs(ctx, env, stamp); err != nil {
		return err
	}

	if err := touchSecrets(ctx, env, stamp); err != nil {
		return err
	}

	if err := touchServices(ctx, env, stamp); err != nil {
		return err
	}

	return touchNodes(ctx, env, stamp)
}

func touchConfigs(ctx context.Context, env *harness.Env, stamp string) error {
	configs, err := env.Docker.ConfigList(ctx, swarm.ConfigListOptions{})
	if err != nil {
		return fmt.Errorf("ConfigList: %w", err)
	}

	for _, cfg := range configs {
		if strings.HasPrefix(cfg.Spec.Name, pageFillerPrefix) {
			continue
		}

		cfg.Spec.Labels = withTouch(cfg.Spec.Labels, stamp)

		if err := env.Docker.ConfigUpdate(ctx, cfg.ID, cfg.Version, cfg.Spec); err != nil {
			return fmt.Errorf("ConfigUpdate %s: %w", cfg.Spec.Name, err)
		}
	}

	return nil
}

func touchSecrets(ctx context.Context, env *harness.Env, stamp string) error {
	secrets, err := env.Docker.SecretList(ctx, swarm.SecretListOptions{})
	if err != nil {
		return fmt.Errorf("SecretList: %w", err)
	}

	for _, sec := range secrets {
		sec.Spec.Labels = withTouch(sec.Spec.Labels, stamp)

		if err := env.Docker.SecretUpdate(ctx, sec.ID, sec.Version, sec.Spec); err != nil {
			return fmt.Errorf("SecretUpdate %s: %w", sec.Spec.Name, err)
		}
	}

	return nil
}

// touchServices writes Spec.Labels rather than the task template, so no task
// is recreated.
func touchServices(ctx context.Context, env *harness.Env, stamp string) error {
	services, err := env.Docker.ServiceList(ctx, swarm.ServiceListOptions{})
	if err != nil {
		return fmt.Errorf("ServiceList: %w", err)
	}

	for _, svc := range services {
		svc.Spec.Labels = withTouch(svc.Spec.Labels, stamp)

		if _, err := env.Docker.ServiceUpdate(
			ctx,
			svc.ID,
			svc.Version,
			svc.Spec,
			swarm.ServiceUpdateOptions{},
		); err != nil {
			return fmt.Errorf("ServiceUpdate %s: %w", svc.Spec.Name, err)
		}
	}

	return nil
}

func touchNodes(ctx context.Context, env *harness.Env, stamp string) error {
	nodes, err := env.Docker.NodeList(ctx, swarm.NodeListOptions{})
	if err != nil {
		return fmt.Errorf("NodeList: %w", err)
	}

	for _, node := range nodes {
		node.Spec.Labels = withTouch(node.Spec.Labels, stamp)

		if err := env.Docker.NodeUpdate(ctx, node.ID, node.Version, node.Spec); err != nil {
			return fmt.Errorf("NodeUpdate %s: %w", node.ID, err)
		}
	}

	return nil
}

func withTouch(labels map[string]string, stamp string) map[string]string {
	if labels == nil {
		labels = map[string]string{}
	}

	labels[touchLabel] = stamp

	return labels
}
