package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/docker"
)

const socketBoundID = "svc1abcdef"

func hostAccessSpecs() (plain, bound swarm.ServiceSpec) {
	plain = swarm.ServiceSpec{
		Name: "web",
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "nginx"},
		},
	}
	bound = plain
	bound.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Image: "nginx",
		Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: "/var/run/docker.sock",
			Target: "/var/run/docker.sock",
		}},
	}

	return plain, bound
}

// hostAccessWriteClient answers every write the host-access tools make with
// record, which notes the call and the version it was pinned to.
func hostAccessWriteClient(
	record func(context.Context) (swarm.Service, error),
) *fakeWriteClient {
	return &fakeWriteClient{
		updateServiceImageFn: func(ctx context.Context, _, _ string) (swarm.Service, error) {
			return record(ctx)
		},
		rollbackServiceFn: func(ctx context.Context, _ string) (swarm.Service, error) {
			return record(ctx)
		},
		updateServicePlaceFn: func(
			ctx context.Context, _ string, _ *swarm.Placement,
		) (swarm.Service, error) {
			return record(ctx)
		},
		updateServiceHealthFn: func(
			ctx context.Context, _ string, _ *container.HealthConfig,
		) (swarm.Service, error) {
			return record(ctx)
		},
		updateServiceContainerFn: func(
			ctx context.Context, _ string, _ func(*swarm.ContainerSpec),
		) (swarm.Service, error) {
			return record(ctx)
		},
		updateServiceSecretsFn: func(
			ctx context.Context, _ string, _ []*swarm.SecretReference,
		) (swarm.Service, error) {
			return record(ctx)
		},
		updateServiceConfigsFn: func(
			ctx context.Context, _ string, _ []*swarm.ConfigReference,
		) (swarm.Service, error) {
			return record(ctx)
		},
		updateServiceMountsFn: func(
			ctx context.Context, _ string, _ []mount.Mount,
		) (swarm.Service, error) {
			return record(ctx)
		},
		updateServiceEnvFn: func(
			ctx context.Context, _ string, _ map[string]string,
		) (swarm.Service, error) {
			return record(ctx)
		},
		updateServicePortsFn: func(
			ctx context.Context, _ string, _ []swarm.PortConfig,
		) (swarm.Service, error) {
			return record(ctx)
		},
		updateServiceLabelsFn: func(
			ctx context.Context, _ string, _ map[string]string,
		) (swarm.Service, error) {
			return record(ctx)
		},
	}
}

// Host access a service already holds is exempt from the mount and capability
// checks, so changing what runs with it needs the impactful tier instead.
func TestHostAccessHeldRequiresImpactfulLevel(t *testing.T) {
	plain, bound := hostAccessSpecs()
	current := swarm.Service{ID: socketBoundID, Spec: bound, PreviousSpec: &plain}
	previous := swarm.Service{ID: socketBoundID, Spec: plain, PreviousSpec: &bound}
	unbound := swarm.Service{ID: socketBoundID, Spec: plain, PreviousSpec: &plain}

	const (
		tier1 = config.OpsOperational
		tier2 = config.OpsConfiguration
	)
	section := func(name, value string) string {
		return `{"name":"update_service","arguments":{"id":"web","section":"` + name +
			`","value":` + value + `}}`
	}
	socketMount := `{"type":"bind","source":"/var/run/docker.sock","target":"/var/run/docker.sock"}`

	cases := []struct {
		name    string
		svc     swarm.Service
		call    string
		below   config.OperationsLevel
		refused bool
	}{
		{
			"image", current,
			`{"name":"update_service_image","arguments":{"id":"web","image":"evil"}}`,
			tier1, true,
		},
		{
			"image with a bind only in the previous spec", previous,
			`{"name":"update_service_image","arguments":{"id":"web","image":"nginx:2"}}`,
			tier1, false,
		},
		{
			"rollback restoring a bind", previous,
			`{"name":"rollback_service","arguments":{"id":"web"}}`,
			tier1, true,
		},
		{
			"command", current, section("command", `{"command":["sh"]}`),
			tier2, true,
		},
		{
			"command unchanged", current, section("command", `{}`),
			tier2, false,
		},
		{
			"placement", current, section("placement", `{"Constraints":["node.role==manager"]}`),
			tier2, true,
		},
		{
			"healthcheck test", current, section("healthcheck", `{"test":["CMD","sh"]}`),
			tier2, true,
		},
		{
			"healthcheck interval only", current, section("healthcheck", `{"interval":"10s"}`),
			tier2, false,
		},
		{
			"secret added", current,
			`{"name":"update_service_secrets","arguments":{"id":"web",` +
				`"secrets":[{"name":"s","target":"/docker-entrypoint.sh"}]}}`,
			tier2, true,
		},
		{
			"config added", current,
			`{"name":"update_service_configs","arguments":{"id":"web",` +
				`"configs":[{"name":"c","target":"/docker-entrypoint.sh"}]}}`,
			tier2, true,
		},
		{
			"mount added beside the bind", current,
			`{"name":"update_service_mounts","arguments":{"id":"web","mounts":[` + socketMount +
				`,{"type":"tmpfs","target":"/docker-entrypoint.sh"}]}}`,
			tier2, true,
		},
		{
			"mounts unchanged",
			current,
			`{"name":"update_service_mounts","arguments":{"id":"web","mounts":[` + socketMount + `]}}`,
			tier2,
			false,
		},
		{
			"image without host access", unbound,
			`{"name":"update_service_image","arguments":{"id":"web","image":"nginx:2"}}`,
			tier1, false,
		},
		{
			"rollback without host access", unbound,
			`{"name":"rollback_service","arguments":{"id":"web"}}`,
			tier1, false,
		},
		{
			"environment", current, section("env", `{"DOCKER_HOST":"tcp://0.0.0.0:2375"}`),
			tier2, true,
		},
		{
			"environment unchanged", current, section("env", `{}`),
			tier2, false,
		},
		{
			"environment without host access", unbound, section("env", `{"FOO":"bar"}`),
			tier2, false,
		},
		{
			"published port", current,
			section("ports", `[{"Protocol":"tcp","TargetPort":2375,"PublishedPort":2375}]`),
			tier2, true,
		},
		{
			"ports unchanged", current, section("ports", `[]`),
			tier2, false,
		},
		{
			"service label", current, section("labels", `{"traefik.enable":"true"}`),
			tier2, true,
		},
		{
			"service labels unchanged", current, section("labels", `{}`),
			tier2, false,
		},
		{
			"service label without host access", unbound,
			section("labels", `{"traefik.enable":"true"}`),
			tier2, false,
		},
	}

	for _, tc := range cases {
		for _, level := range []config.OperationsLevel{tc.below, config.OpsImpactful} {
			t.Run(fmt.Sprintf("%s at level %d", tc.name, level), func(t *testing.T) {
				c := cache.New(nil)
				tc.svc.Version = swarm.Version{Index: 7}
				c.SetService(tc.svc)
				c.SetSecret(swarm.Secret{ID: "sec1", Spec: swarm.SecretSpec{
					Annotations: swarm.Annotations{Name: "s"},
				}})
				c.SetConfig(swarm.Config{ID: "cfg1", Spec: swarm.ConfigSpec{
					Annotations: swarm.Annotations{Name: "c"},
				}})

				written := false
				var pinned swarm.Version
				wc := hostAccessWriteClient(func(ctx context.Context) (swarm.Service, error) {
					written = true
					pinned, _ = docker.PinnedVersion(ctx, "service", socketBoundID)
					return tc.svc, nil
				})

				handler := newToolTestServer(t, c, wc, level).Handler()

				if !tc.refused || level >= config.OpsImpactful {
					callTool(t, handler, tc.call)
					if !written {
						t.Fatal("the write did not run")
					}

					// Decided inside the write, against Docker's spec: no
					// pin to what the cache held is needed to keep it honest.
					if pinned.Index != 0 {
						t.Errorf("write pinned to %v, want it unpinned", pinned)
					}

					return
				}

				raw := callToolExpectingError(t, handler, tc.call)
				if written {
					t.Error("the write ran below level 3")
				}
				if !strings.Contains(raw, "requires operations level 3") {
					t.Errorf("the error does not name the tier: %s", raw)
				}
			})
		}
	}
}

// Docker takes an ID prefix the cache does not resolve. Below level 3 the
// check needs the service it guards, so a write naming one is refused.
func TestHostAccessHeldRefusesAnUnresolvedService(t *testing.T) {
	_, bound := hostAccessSpecs()
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: socketBoundID, Spec: bound, Version: swarm.Version{Index: 7}})

	written := false
	wc := hostAccessWriteClient(func(context.Context) (swarm.Service, error) {
		written = true
		return swarm.Service{ID: socketBoundID, Spec: bound}, nil
	})
	handler := newToolTestServer(t, c, wc, config.OpsOperational).Handler()

	for _, call := range []string{
		`{"name":"update_service_image","arguments":{"id":"svc1abc","image":"evil"}}`,
		`{"name":"rollback_service","arguments":{"id":"svc1abc"}}`,
	} {
		raw := callToolExpectingError(t, handler, call)
		if written {
			t.Fatalf("the write ran for an unresolved ID: %s", call)
		}
		if !strings.Contains(raw, "not found") {
			t.Errorf("the error does not say the service was not found: %s", raw)
		}
	}
}
