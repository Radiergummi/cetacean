package docker

import (
	"context"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
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

func (c *Client) ServiceLogs(ctx context.Context, serviceID string, tail string, follow bool, since, until string) (io.ReadCloser, error) {
	if tail == "" {
		tail = "200"
	}
	return c.docker.ServiceLogs(ctx, serviceID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
		Timestamps: true,
		Follow:     follow,
		Since:      since,
		Until:      until,
	})
}

func (c *Client) TaskLogs(ctx context.Context, taskID string, tail string, follow bool, since, until string) (io.ReadCloser, error) {
	if tail == "" {
		tail = "200"
	}
	return c.docker.TaskLogs(ctx, taskID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
		Timestamps: true,
		Follow:     follow,
		Since:      since,
		Until:      until,
	})
}
