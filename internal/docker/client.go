package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"

	"github.com/radiergummi/cetacean/internal/cache"
)

// LogKind selects between service and task logs.
type LogKind int

const (
	ServiceLog LogKind = iota
	TaskLog
)

type Client struct {
	docker *client.Client
}

func NewClient(host string) (*Client, error) {
	opts := []client.Opt{
		client.WithAPIVersionNegotiation(),
	}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &Client{docker: c}, nil
}

func (c *Client) Close() error {
	return c.docker.Close()
}

func (c *Client) ListNodes(ctx context.Context) ([]swarm.Node, error) {
	return c.docker.NodeList(ctx, swarm.NodeListOptions{})
}

func (c *Client) ListServices(ctx context.Context) ([]swarm.Service, error) {
	return c.docker.ServiceList(ctx, swarm.ServiceListOptions{})
}

func (c *Client) ListTasks(ctx context.Context) ([]swarm.Task, error) {
	return c.docker.TaskList(ctx, swarm.TaskListOptions{})
}

func (c *Client) ListConfigs(ctx context.Context) ([]swarm.Config, error) {
	return c.docker.ConfigList(ctx, swarm.ConfigListOptions{})
}

func (c *Client) ListSecrets(ctx context.Context) ([]swarm.Secret, error) {
	return c.docker.SecretList(ctx, swarm.SecretListOptions{})
}

func (c *Client) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	return c.docker.NetworkList(ctx, network.ListOptions{})
}

func (c *Client) ListVolumes(ctx context.Context) ([]volume.Volume, error) {
	resp, err := c.docker.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]volume.Volume, len(resp.Volumes))
	for i, v := range resp.Volumes {
		out[i] = *v
	}
	return out, nil
}

func (c *Client) InspectNode(ctx context.Context, id string) (swarm.Node, error) {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, id)
	return node, err
}

func (c *Client) InspectService(ctx context.Context, id string) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	return svc, err
}

func (c *Client) InspectTask(ctx context.Context, id string) (swarm.Task, error) {
	task, _, err := c.docker.TaskInspectWithRaw(ctx, id)
	return task, err
}

func (c *Client) InspectConfig(ctx context.Context, id string) (swarm.Config, error) {
	cfg, _, err := c.docker.ConfigInspectWithRaw(ctx, id)
	return cfg, err
}

func (c *Client) InspectSecret(ctx context.Context, id string) (swarm.Secret, error) {
	sec, _, err := c.docker.SecretInspectWithRaw(ctx, id)
	return sec, err
}

func (c *Client) InspectNetwork(ctx context.Context, id string) (network.Summary, error) {
	return c.docker.NetworkInspect(ctx, id, network.InspectOptions{})
}

func (c *Client) Events(ctx context.Context) (<-chan events.Message, <-chan error) {
	return c.docker.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("type", string(events.ServiceEventType)),
			filters.Arg("type", string(events.NodeEventType)),
			filters.Arg("type", string(events.SecretEventType)),
			filters.Arg("type", string(events.ConfigEventType)),
			filters.Arg("type", string(events.NetworkEventType)),
			filters.Arg("type", string(events.VolumeEventType)),
			filters.Arg("type", string(events.ContainerEventType)),
		),
	})
}

func (c *Client) InspectVolume(ctx context.Context, name string) (volume.Volume, error) {
	return c.docker.VolumeInspect(ctx, name)
}

func (c *Client) SwarmInspect(ctx context.Context) (swarm.Swarm, error) {
	return c.docker.SwarmInspect(ctx)
}

func (c *Client) PluginList(ctx context.Context) (types.PluginsListResponse, error) {
	return c.docker.PluginList(ctx, filters.Args{})
}

// FullSync fetches all swarm resources in parallel. Individual resource type
// failures are logged and their Has* flag stays false so the cache preserves
// existing data for that type.
func (c *Client) FullSync(ctx context.Context) cache.FullSyncData {
	type result struct {
		name string
		err  error
	}

	var data cache.FullSyncData
	var mu sync.Mutex
	ch := make(chan result, 7)

	fetch := func(name string, fn func() error) {
		go func() {
			ch <- result{name, fn()}
		}()
	}

	fetch("nodes", func() error {
		nodes, err := c.ListNodes(ctx)
		if err != nil {
			return err
		}
		mu.Lock()
		data.Nodes, data.HasNodes = nodes, true
		mu.Unlock()
		return nil
	})
	fetch("services", func() error {
		services, err := c.ListServices(ctx)
		if err != nil {
			return err
		}
		mu.Lock()
		data.Services, data.HasServices = services, true
		mu.Unlock()
		return nil
	})
	fetch("tasks", func() error {
		tasks, err := c.ListTasks(ctx)
		if err != nil {
			return err
		}
		mu.Lock()
		data.Tasks, data.HasTasks = tasks, true
		mu.Unlock()
		return nil
	})
	fetch("configs", func() error {
		configs, err := c.ListConfigs(ctx)
		if err != nil {
			return err
		}
		mu.Lock()
		data.Configs, data.HasConfigs = configs, true
		mu.Unlock()
		return nil
	})
	fetch("secrets", func() error {
		secrets, err := c.ListSecrets(ctx)
		if err != nil {
			return err
		}
		mu.Lock()
		data.Secrets, data.HasSecrets = secrets, true
		mu.Unlock()
		return nil
	})
	fetch("networks", func() error {
		networks, err := c.ListNetworks(ctx)
		if err != nil {
			return err
		}
		mu.Lock()
		data.Networks, data.HasNetworks = networks, true
		mu.Unlock()
		return nil
	})
	fetch("volumes", func() error {
		volumes, err := c.ListVolumes(ctx)
		if err != nil {
			return err
		}
		mu.Lock()
		data.Volumes, data.HasVolumes = volumes, true
		mu.Unlock()
		return nil
	})

	for range 7 {
		r := <-ch
		if r.err != nil {
			slog.Warn("full sync resource failed", "resource", r.name, "error", r.err)
		}
	}

	return data
}

// Inspect fetches a single resource by its event type and ID. Returns the
// typed resource as an any. The caller type-switches to apply it to the store.
func (c *Client) Inspect(ctx context.Context, resourceType events.Type, id string) (any, error) {
	switch resourceType {
	case events.NodeEventType:
		return c.InspectNode(ctx, id)
	case events.ServiceEventType:
		return c.InspectService(ctx, id)
	case events.ConfigEventType:
		return c.InspectConfig(ctx, id)
	case events.SecretEventType:
		return c.InspectSecret(ctx, id)
	case events.NetworkEventType:
		return c.InspectNetwork(ctx, id)
	case events.VolumeEventType:
		return c.InspectVolume(ctx, id)
	case "task":
		return c.InspectTask(ctx, id)
	default:
		return nil, fmt.Errorf("unknown resource type: %s", resourceType)
	}
}

func (c *Client) DiskUsage(ctx context.Context) (types.DiskUsage, error) {
	return c.docker.DiskUsage(ctx, types.DiskUsageOptions{})
}

// LocalNodeID returns the swarm node ID of the Docker host this client is connected to.
func (c *Client) LocalNodeID(ctx context.Context) (string, error) {
	info, err := c.docker.Info(ctx)
	if err != nil {
		return "", err
	}
	return info.Swarm.NodeID, nil
}

// Logs fetches multiplexed logs for a service or task.
func (c *Client) Logs(
	ctx context.Context,
	kind LogKind,
	id string,
	tail string,
	follow bool,
	since, until string,
) (io.ReadCloser, error) {
	if tail == "" {
		tail = "200"
	}
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
		Timestamps: true,
		Details:    true,
		Follow:     follow,
		Since:      since,
		Until:      until,
	}
	switch kind {
	case ServiceLog:
		return c.docker.ServiceLogs(ctx, id, opts)
	case TaskLog:
		return c.docker.TaskLogs(ctx, id, opts)
	default:
		return nil, fmt.Errorf("unknown log kind: %d", kind)
	}
}

func (c *Client) ScaleService(
	ctx context.Context,
	id string,
	replicas uint64,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.Mode.Replicated == nil {
		return swarm.Service{}, errdefs.InvalidParameter(
			fmt.Errorf("cannot scale a global-mode service"),
		)
	}
	svc.Spec.Mode.Replicated.Replicas = &replicas
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceImage(
	ctx context.Context,
	id string,
	image string,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.TaskTemplate.ContainerSpec == nil {
		return swarm.Service{}, errdefs.InvalidParameter(
			fmt.Errorf("service has no container spec"),
		)
	}
	svc.Spec.TaskTemplate.ContainerSpec.Image = image
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) RollbackService(ctx context.Context, id string) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.PreviousSpec == nil {
		return swarm.Service{}, errdefs.InvalidParameter(
			fmt.Errorf("service has no previous spec to rollback to"),
		)
	}
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{
		Rollback: "previous",
	})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) RestartService(ctx context.Context, id string) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.TaskTemplate.ForceUpdate++
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateNodeAvailability(
	ctx context.Context,
	id string,
	availability swarm.NodeAvailability,
) (swarm.Node, error) {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, id)
	if err != nil {
		return swarm.Node{}, err
	}
	node.Spec.Availability = availability
	err = c.docker.NodeUpdate(ctx, node.ID, node.Version, node.Spec)
	if err != nil {
		return swarm.Node{}, err
	}
	return c.InspectNode(ctx, id)
}

func (c *Client) UpdateNodeRole(
	ctx context.Context,
	id string,
	role swarm.NodeRole,
) (swarm.Node, error) {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, id)
	if err != nil {
		return swarm.Node{}, err
	}
	node.Spec.Role = role
	err = c.docker.NodeUpdate(ctx, node.ID, node.Version, node.Spec)
	if err != nil {
		return swarm.Node{}, err
	}
	return c.InspectNode(ctx, id)
}

func (c *Client) RemoveNode(ctx context.Context, id string) error {
	return c.docker.NodeRemove(ctx, id, swarm.NodeRemoveOptions{Force: false})
}

func (c *Client) RemoveTask(ctx context.Context, id string) error {
	task, _, err := c.docker.TaskInspectWithRaw(ctx, id)
	if err != nil {
		return err
	}
	if task.Status.ContainerStatus == nil || task.Status.ContainerStatus.ContainerID == "" {
		return errdefs.NotFound(fmt.Errorf("task has no running container"))
	}
	return c.docker.ContainerRemove(
		ctx,
		task.Status.ContainerStatus.ContainerID,
		container.RemoveOptions{Force: true},
	)
}

func (c *Client) RemoveService(ctx context.Context, id string) error {
	return c.docker.ServiceRemove(ctx, id)
}

func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.docker.NetworkRemove(ctx, id)
}

func (c *Client) RemoveConfig(ctx context.Context, id string) error {
	return c.docker.ConfigRemove(ctx, id)
}

func (c *Client) RemoveSecret(ctx context.Context, id string) error {
	return c.docker.SecretRemove(ctx, id)
}

func (c *Client) UpdateServiceEnv(
	ctx context.Context,
	id string,
	env map[string]string,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.TaskTemplate.ContainerSpec == nil {
		return swarm.Service{}, errdefs.InvalidParameter(
			fmt.Errorf("service has no container spec"),
		)
	}
	envSlice := make([]string, 0, len(env))
	for k, v := range env {
		envSlice = append(envSlice, k+"="+v)
	}
	sort.Strings(envSlice)
	svc.Spec.TaskTemplate.ContainerSpec.Env = envSlice
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateNodeLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Node, error) {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, id)
	if err != nil {
		return swarm.Node{}, err
	}
	node.Spec.Labels = labels
	err = c.docker.NodeUpdate(ctx, node.ID, node.Version, node.Spec)
	if err != nil {
		return swarm.Node{}, err
	}
	return c.InspectNode(ctx, id)
}

func (c *Client) UpdateServiceLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.Labels = labels
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceHealthcheck(
	ctx context.Context,
	id string,
	hc *container.HealthConfig,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.TaskTemplate.ContainerSpec == nil {
		return swarm.Service{}, errdefs.InvalidParameter(
			fmt.Errorf("service has no container spec"),
		)
	}
	svc.Spec.TaskTemplate.ContainerSpec.Healthcheck = hc
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceEndpointMode(
	ctx context.Context,
	id string,
	mode swarm.ResolutionMode,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.EndpointSpec == nil {
		svc.Spec.EndpointSpec = &swarm.EndpointSpec{}
	}
	svc.Spec.EndpointSpec.Mode = mode
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceMode(
	ctx context.Context,
	id string,
	mode swarm.ServiceMode,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.Mode = mode
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceResources(
	ctx context.Context,
	id string,
	resources *swarm.ResourceRequirements,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.TaskTemplate.Resources = resources
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServicePlacement(
	ctx context.Context,
	id string,
	placement *swarm.Placement,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.TaskTemplate.Placement = placement
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServicePorts(
	ctx context.Context,
	id string,
	ports []swarm.PortConfig,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.EndpointSpec == nil {
		svc.Spec.EndpointSpec = &swarm.EndpointSpec{}
	}
	svc.Spec.EndpointSpec.Ports = ports
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceUpdatePolicy(
	ctx context.Context,
	id string,
	policy *swarm.UpdateConfig,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.UpdateConfig = policy
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceRollbackPolicy(
	ctx context.Context,
	id string,
	policy *swarm.UpdateConfig,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.RollbackConfig = policy
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceLogDriver(
	ctx context.Context,
	id string,
	driver *swarm.Driver,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.TaskTemplate.LogDriver = driver
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceConfigs(
	ctx context.Context,
	id string,
	configs []*swarm.ConfigReference,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.TaskTemplate.ContainerSpec == nil {
		svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{}
	}
	svc.Spec.TaskTemplate.ContainerSpec.Configs = configs
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceSecrets(
	ctx context.Context,
	id string,
	secrets []*swarm.SecretReference,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.TaskTemplate.ContainerSpec == nil {
		svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{}
	}
	svc.Spec.TaskTemplate.ContainerSpec.Secrets = secrets
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceNetworks(
	ctx context.Context,
	id string,
	networks []swarm.NetworkAttachmentConfig,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.TaskTemplate.Networks = networks
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}

func (c *Client) UpdateServiceContainerConfig(
	ctx context.Context,
	id string,
	apply func(spec *swarm.ContainerSpec),
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	apply(svc.Spec.TaskTemplate.ContainerSpec)
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
