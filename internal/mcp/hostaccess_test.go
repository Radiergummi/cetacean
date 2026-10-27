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

// Host access a service already holds is exempt from the mount and capability
// checks, so changing what runs with it needs the impactful tier instead.
func TestHostAccessHeldRequiresImpactfulLevel(t *testing.T) {
	plain := swarm.ServiceSpec{
		Name: "web",
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "nginx"},
		},
	}
	bound := plain
	bound.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Image: "nginx",
		Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: "/var/run/docker.sock",
			Target: "/var/run/docker.sock",
		}},
	}
	current := swarm.Service{ID: "svc1", Spec: bound, PreviousSpec: &plain}
	previous := swarm.Service{ID: "svc1", Spec: plain, PreviousSpec: &bound}
	unbound := swarm.Service{ID: "svc1", Spec: plain, PreviousSpec: &plain}

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
			config.OpsOperational, true,
		},
		{
			"rollback restoring a bind", previous,
			`{"name":"rollback_service","arguments":{"id":"web"}}`,
			config.OpsOperational, true,
		},
		{
			"command", current,
			`{"name":"update_service","arguments":{"id":"web","section":"command",` +
				`"value":{"command":["sh"]}}}`,
			config.OpsConfiguration, true,
		},
		{
			"placement", current,
			`{"name":"update_service","arguments":{"id":"web","section":"placement",` +
				`"value":{"Constraints":["node.role==manager"]}}}`,
			config.OpsConfiguration, true,
		},
		{
			"healthcheck test", current,
			`{"name":"update_service","arguments":{"id":"web","section":"healthcheck",` +
				`"value":{"test":["CMD","sh"]}}}`,
			config.OpsConfiguration, true,
		},
		{
			"healthcheck interval only", current,
			`{"name":"update_service","arguments":{"id":"web","section":"healthcheck",` +
				`"value":{"interval":"10s"}}}`,
			config.OpsConfiguration, false,
		},
		{
			"image without host access", unbound,
			`{"name":"update_service_image","arguments":{"id":"web","image":"nginx:2"}}`,
			config.OpsOperational, false,
		},
		{
			"rollback without host access", unbound,
			`{"name":"rollback_service","arguments":{"id":"web"}}`,
			config.OpsOperational, false,
		},
	}

	for _, tc := range cases {
		for _, level := range []config.OperationsLevel{tc.below, config.OpsImpactful} {
			t.Run(fmt.Sprintf("%s at level %d", tc.name, level), func(t *testing.T) {
				c := cache.New(nil)
				tc.svc.Version = swarm.Version{Index: 7}
				c.SetService(tc.svc)

				written := false
				var pinned swarm.Version
				write := func(ctx context.Context) (swarm.Service, error) {
					written = true
					pinned, _ = docker.PinnedVersion(ctx, "service", "svc1")
					return tc.svc, nil
				}
				wc := &fakeWriteClient{
					updateServiceImageFn: func(ctx context.Context, _, _ string) (swarm.Service, error) {
						return write(ctx)
					},
					rollbackServiceFn: func(ctx context.Context, _ string) (swarm.Service, error) {
						return write(ctx)
					},
					updateServicePlaceFn: func(
						ctx context.Context, _ string, _ *swarm.Placement,
					) (swarm.Service, error) {
						return write(ctx)
					},
					updateServiceHealthFn: func(
						ctx context.Context, _ string, _ *container.HealthConfig,
					) (swarm.Service, error) {
						return write(ctx)
					},
					updateServiceContainerFn: func(
						ctx context.Context, _ string, _ func(*swarm.ContainerSpec),
					) (swarm.Service, error) {
						return write(ctx)
					},
				}

				handler := newToolTestServer(t, c, wc, level).Handler()

				if !tc.refused || level >= config.OpsImpactful {
					callTool(t, handler, tc.call)
					if !written || pinned.Index != 7 {
						t.Errorf("written=%v pinned=%v, want a write pinned to 7", written, pinned)
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
