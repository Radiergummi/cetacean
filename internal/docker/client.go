package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/versions"
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
	docker       *client.Client
	attributions *cache.Attributions
}

// The Docker API this client speaks, and the Engine release that first served
// it. Pinned rather than negotiated: negotiating down would leave Cetacean
// partially working against an older daemon without saying so.
const (
	RequiredAPIVersion   = "1.46"
	MinimumEngineVersion = "26.1"
)

// UnsupportedEngineError reports a daemon too old to serve RequiredAPIVersion.
type UnsupportedEngineError struct {
	DaemonAPIVersion string
}

func (e *UnsupportedEngineError) Error() string {
	return fmt.Sprintf(
		"this Docker daemon serves API %s, and Cetacean requires %s "+
			"(Docker Engine %s or newer); every request would be refused as too new",
		e.DaemonAPIVersion, RequiredAPIVersion, MinimumEngineVersion,
	)
}

// CheckAPIVersion reports whether the daemon can serve RequiredAPIVersion. An
// unreachable daemon is not an error: the socket may not be mounted yet, and
// the watcher retries.
func (c *Client) CheckAPIVersion(ctx context.Context) error {
	// Ping reads Api-Version from the headers even when the daemon refuses the
	// request as too new, so the error it returns is the case being checked.
	ping, _ := c.docker.Ping(ctx)

	if ping.APIVersion == "" {
		return nil
	}

	if versions.LessThan(ping.APIVersion, RequiredAPIVersion) {
		return &UnsupportedEngineError{DaemonAPIVersion: ping.APIVersion}
	}

	return nil
}

// TLS names the PEM files for reaching an engine over tcp:// with TLS. An empty
// CA trusts the system roots; an empty Cert and Key present no certificate.
type TLS struct {
	CA, Cert, Key string
}

func NewClient(host string, tls TLS) (*Client, error) {
	opts := []client.Opt{
		client.WithVersion(RequiredAPIVersion),
	}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	if tls != (TLS{}) {
		opts = append(opts, client.WithTLSClientConfig(tls.CA, tls.Cert, tls.Key))
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

func (c *Client) ListTasksIn(ctx context.Context, scope TaskScope) ([]swarm.Task, error) {
	args := filters.NewArgs()
	if scope.Service != "" {
		args.Add("service", scope.Service)
	}
	if scope.Node != "" {
		args.Add("node", scope.Node)
	}

	return c.docker.TaskList(ctx, swarm.TaskListOptions{Filters: args})
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

func (c *Client) Events(
	ctx context.Context,
	since time.Time,
) (<-chan events.Message, <-chan error) {
	var sinceArg string
	if !since.IsZero() {
		sinceArg = fmt.Sprintf("%d.%09d", since.Unix(), since.Nanosecond())
	}

	return c.docker.Events(ctx, events.ListOptions{
		Since: sinceArg,
		Filters: filters.NewArgs(
			filters.Arg("type", string(events.ServiceEventType)),
			filters.Arg("type", string(events.NodeEventType)),
			filters.Arg("type", string(events.SecretEventType)),
			filters.Arg("type", string(events.ConfigEventType)),
			filters.Arg("type", string(events.NetworkEventType)),
			filters.Arg("type", string(events.VolumeEventType)),
			filters.Arg("type", string(events.ContainerEventType)),
			filters.Arg("type", "task"),
		),
	})
}

func (c *Client) InspectVolume(ctx context.Context, name string) (volume.Volume, error) {
	return c.docker.VolumeInspect(ctx, name)
}

func (c *Client) SwarmInspect(ctx context.Context) (swarm.Swarm, error) {
	return c.docker.SwarmInspect(ctx)
}

func (c *Client) UpdateSwarm(
	ctx context.Context,
	spec swarm.Spec,
	version swarm.Version,
	flags swarm.UpdateFlags,
) error {
	return c.docker.SwarmUpdate(ctx, version, spec, flags)
}

func (c *Client) GetUnlockKey(ctx context.Context) (string, error) {
	resp, err := c.docker.SwarmGetUnlockKey(ctx)
	if err != nil {
		return "", err
	}
	return resp.UnlockKey, nil
}

func (c *Client) UnlockSwarm(ctx context.Context, key string) error {
	return c.docker.SwarmUnlock(ctx, swarm.UnlockRequest{UnlockKey: key})
}

func (c *Client) PluginList(ctx context.Context) (types.PluginsListResponse, error) {
	return c.docker.PluginList(ctx, filters.Args{})
}

func (c *Client) PluginInspect(ctx context.Context, name string) (*types.Plugin, error) {
	p, _, err := c.docker.PluginInspectWithRaw(ctx, name)
	return p, err
}

func (c *Client) PluginEnable(ctx context.Context, name string) error {
	return c.docker.PluginEnable(ctx, name, types.PluginEnableOptions{Timeout: 30})
}

func (c *Client) PluginDisable(ctx context.Context, name string) error {
	return c.docker.PluginDisable(ctx, name, types.PluginDisableOptions{})
}

func (c *Client) PluginRemove(ctx context.Context, name string, force bool) error {
	return c.docker.PluginRemove(ctx, name, types.PluginRemoveOptions{Force: force})
}

// ErrPluginPrivilegesChanged reports a plugin that now requires privileges
// other than the ones the caller reviewed and granted.
var ErrPluginPrivilegesChanged = fmt.Errorf(
	"the plugin requires different privileges from those granted: %w",
	cerrdefs.ErrFailedPrecondition,
)

// grantOnly accepts exactly the privileges the caller reviewed. The engine
// sends what it accepts to the pull and refuses a plugin needing any other
// set, so a reference republished after the review cannot widen them.
func grantOnly(approved types.PluginPrivileges) func(
	context.Context,
	types.PluginPrivileges,
) (bool, error) {
	return func(_ context.Context, required types.PluginPrivileges) (bool, error) {
		if !samePrivileges(approved, required) {
			return false, ErrPluginPrivilegesChanged
		}

		return true, nil
	}
}

// samePrivileges compares two privilege sets regardless of order.
func samePrivileges(a, b types.PluginPrivileges) bool {
	key := func(privileges types.PluginPrivileges) []string {
		keys := make([]string, 0, len(privileges))
		for _, p := range privileges {
			values := slices.Clone(p.Value)
			slices.Sort(values)
			keys = append(keys, p.Name+"\x00"+strings.Join(values, "\x00"))
		}
		slices.Sort(keys)

		return keys
	}

	return slices.Equal(key(a), key(b))
}

func (c *Client) PluginInstall(
	ctx context.Context,
	remote string,
	approved types.PluginPrivileges,
) (*types.Plugin, error) {
	rc, err := c.docker.PluginInstall(ctx, remote, types.PluginInstallOptions{
		RemoteRef:             remote,
		AcceptPermissionsFunc: grantOnly(approved),
	})
	if err != nil {
		return nil, err
	}
	_, _ = io.Copy(io.Discard, rc)
	rc.Close()

	return c.PluginInspect(ctx, remote)
}

func (c *Client) PluginUpgrade(
	ctx context.Context,
	name, remote string,
	approved types.PluginPrivileges,
) error {
	rc, err := c.docker.PluginUpgrade(ctx, name, types.PluginInstallOptions{
		RemoteRef:             remote,
		AcceptPermissionsFunc: grantOnly(approved),
	})
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, rc)
	rc.Close()
	return nil
}

// PluginPrivileges queries the registry for the privileges a plugin requires.
// The Docker SDK has no public method for this — the internal tryPluginPrivileges
// is unexported and embedded in the PluginInstall flow. We call the Engine API
// directly via the SDK's HTTP client, which handles unix socket transport.
func (c *Client) PluginPrivileges(
	ctx context.Context,
	remote string,
) (types.PluginPrivileges, error) {
	apiVersion := c.docker.ClientVersion()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"http://d/v"+apiVersion+"/plugins/privileges?remote="+url.QueryEscape(remote),
		nil,
	)
	if err != nil {
		return nil, err
	}

	resp, err := c.docker.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("plugin privileges: %s: %s", resp.Status, string(body))
	}

	var privileges types.PluginPrivileges
	if err := json.NewDecoder(resp.Body).Decode(&privileges); err != nil {
		return nil, err
	}
	return privileges, nil
}

func (c *Client) PluginConfigure(ctx context.Context, name string, args []string) error {
	return c.docker.PluginSet(ctx, name, args)
}

// FullSync fetches all swarm resources in parallel. Individual resource type
// failures are logged and their Has* flag stays false so the cache preserves
// existing data for that type.
func (c *Client) FullSync(ctx context.Context) (cache.FullSyncData, error) {
	type result struct {
		name string
		err  error
	}

	var data cache.FullSyncData
	var mu sync.Mutex
	fetchers := []struct {
		name string
		fn   func() error
	}{
		{"nodes", fetchList(ctx, &mu, c.ListNodes, &data.Nodes, &data.HasNodes)},
		{"services", fetchList(ctx, &mu, c.ListServices, &data.Services, &data.HasServices)},
		{"tasks", fetchList(ctx, &mu, c.ListTasks, &data.Tasks, &data.HasTasks)},
		{"configs", fetchList(ctx, &mu, c.ListConfigs, &data.Configs, &data.HasConfigs)},
		{"secrets", fetchList(ctx, &mu, c.ListSecrets, &data.Secrets, &data.HasSecrets)},
		{"networks", fetchList(ctx, &mu, c.ListNetworks, &data.Networks, &data.HasNetworks)},
		{"volumes", fetchList(ctx, &mu, c.ListVolumes, &data.Volumes, &data.HasVolumes)},
	}

	ch := make(chan result, len(fetchers))
	for _, f := range fetchers {
		go func() {
			ch <- result{f.name, f.fn()}
		}()
	}

	failed := make(map[string]error)
	for range fetchers {
		r := <-ch
		if r.err != nil {
			slog.WarnContext(ctx, "full sync resource failed", "resource", r.name, "error", r.err)
			failed[r.name] = r.err
		}
	}

	return data, fullSyncError(failed)
}

func fetchList[T any](
	ctx context.Context,
	mu *sync.Mutex,
	list func(context.Context) ([]T, error),
	dst *[]T,
	has *bool,
) func() error {
	return func() error {
		items, err := list(ctx)
		if err != nil {
			return err
		}
		mu.Lock()
		*dst, *has = items, true
		mu.Unlock()
		return nil
	}
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

// Logs fetches multiplexed or raw (TTY) logs for a service or task.
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
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		if spec.Mode.Replicated == nil {
			return errdefs.InvalidParameter(errors.New("cannot scale a global-mode service"))
		}
		spec.Mode.Replicated.Replicas = &replicas
		return nil
	})
}

func (c *Client) UpdateServiceImage(
	ctx context.Context,
	id string,
	image string,
) (swarm.Service, error) {
	return c.updateContainerSpec(ctx, id, func(cs *swarm.ContainerSpec) error {
		cs.Image = image
		return nil
	})
}

func (c *Client) RollbackService(ctx context.Context, id string) (swarm.Service, error) {
	return c.updateService(
		ctx,
		id,
		swarm.ServiceUpdateOptions{Rollback: "previous"},
		func(svc *swarm.Service) error {
			if svc.PreviousSpec == nil {
				return errdefs.InvalidParameter(
					errors.New("service has no previous spec to rollback to"),
				)
			}
			return nil
		},
	)
}

func (c *Client) RestartService(ctx context.Context, id string) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.ForceUpdate++
		return nil
	})
}

func (c *Client) UpdateNodeAvailability(
	ctx context.Context,
	id string,
	availability swarm.NodeAvailability,
) (swarm.Node, error) {
	return c.updateNodeSpec(ctx, id, func(spec *swarm.NodeSpec) error {
		spec.Availability = availability
		return nil
	})
}

func (c *Client) UpdateNodeRole(
	ctx context.Context,
	id string,
	role swarm.NodeRole,
) (swarm.Node, error) {
	return c.updateNodeSpec(ctx, id, func(spec *swarm.NodeSpec) error {
		spec.Role = role
		return nil
	})
}

// updateNodeSpec is UpdateServiceSpec for nodes: mutate the live spec, write
// it back pinned to the version it was read at, and return the node re-read.
func (c *Client) updateNodeSpec(
	ctx context.Context,
	id string,
	mutate func(spec *swarm.NodeSpec) error,
) (swarm.Node, error) {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, id)
	if err != nil {
		return swarm.Node{}, err
	}
	if err := checkPinned(ctx, "node", node.ID, node.Version); err != nil {
		return swarm.Node{}, err
	}
	if err := mutate(&node.Spec); err != nil {
		return swarm.Node{}, err
	}
	settle := c.expect(ctx, cache.Expectation{
		Type:  cache.EventNode,
		Key:   node.ID,
		After: node.Version.Index,
	})

	err = c.docker.NodeUpdate(ctx, node.ID, node.Version, node.Spec)
	settle(err)

	if err != nil {
		return swarm.Node{}, err
	}
	return c.InspectNode(ctx, id)
}

func (c *Client) RemoveNode(ctx context.Context, id string, force bool) error {
	return c.removing(ctx, cache.EventNode, id, func() error {
		return c.docker.NodeRemove(ctx, id, swarm.NodeRemoveOptions{Force: force})
	})
}

func (c *Client) RemoveTask(ctx context.Context, id string) error {
	task, _, err := c.docker.TaskInspectWithRaw(ctx, id)
	if err != nil {
		return err
	}
	if task.Status.ContainerStatus == nil || task.Status.ContainerStatus.ContainerID == "" {
		return errdefs.NotFound(errors.New("task has no running container"))
	}

	// The task outlives its container; what changes is its state.
	settle := c.expect(ctx, cache.Expectation{
		Type:  cache.EventTask,
		Key:   task.ID,
		After: task.Version.Index,
	})

	err = c.docker.ContainerRemove(
		ctx,
		task.Status.ContainerStatus.ContainerID,
		container.RemoveOptions{Force: true},
	)
	settle(err)

	return err
}

func (c *Client) RemoveService(ctx context.Context, id string) error {
	return c.removing(ctx, cache.EventService, id, func() error {
		return c.docker.ServiceRemove(ctx, id)
	})
}

func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.removing(ctx, cache.EventNetwork, id, func() error {
		return c.docker.NetworkRemove(ctx, id)
	})
}

func (c *Client) RemoveConfig(ctx context.Context, id string) error {
	return c.removing(ctx, cache.EventConfig, id, func() error {
		return c.docker.ConfigRemove(ctx, id)
	})
}

func (c *Client) RemoveSecret(ctx context.Context, id string) error {
	return c.removing(ctx, cache.EventSecret, id, func() error {
		return c.docker.SecretRemove(ctx, id)
	})
}

func (c *Client) CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error) {
	settle := c.expect(ctx, cache.Expectation{
		Type:   cache.EventConfig,
		Key:    spec.Name,
		Action: "create",
	})

	resp, err := c.docker.ConfigCreate(ctx, spec)
	settle(err)

	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error) {
	settle := c.expect(ctx, cache.Expectation{
		Type:   cache.EventSecret,
		Key:    spec.Name,
		Action: "create",
	})

	resp, err := c.docker.SecretCreate(ctx, spec)
	settle(err)

	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) UpdateConfigLabels(
	ctx context.Context,
	id string,
	mutate MapMutator,
) (swarm.Config, error) {
	cfg, _, err := c.docker.ConfigInspectWithRaw(ctx, id)
	if err != nil {
		return swarm.Config{}, err
	}
	if err := checkPinned(ctx, "config", cfg.ID, cfg.Version); err != nil {
		return swarm.Config{}, err
	}
	updated, err := mutate(cfg.Spec.Labels)
	if err != nil {
		return swarm.Config{}, err
	}
	cfg.Spec.Labels = updated
	settle := c.expect(ctx, cache.Expectation{
		Type:  cache.EventConfig,
		Key:   cfg.ID,
		After: cfg.Version.Index,
	})

	err = c.docker.ConfigUpdate(ctx, cfg.ID, cfg.Version, cfg.Spec)
	settle(err)

	if err != nil {
		return swarm.Config{}, err
	}
	return c.InspectConfig(ctx, id)
}

func (c *Client) UpdateSecretLabels(
	ctx context.Context,
	id string,
	mutate MapMutator,
) (swarm.Secret, error) {
	sec, _, err := c.docker.SecretInspectWithRaw(ctx, id)
	if err != nil {
		return swarm.Secret{}, err
	}
	if err := checkPinned(ctx, "secret", sec.ID, sec.Version); err != nil {
		return swarm.Secret{}, err
	}
	updated, err := mutate(sec.Spec.Labels)
	if err != nil {
		return swarm.Secret{}, err
	}
	sec.Spec.Labels = updated
	settle := c.expect(ctx, cache.Expectation{
		Type:  cache.EventSecret,
		Key:   sec.ID,
		After: sec.Version.Index,
	})

	err = c.docker.SecretUpdate(ctx, sec.ID, sec.Version, sec.Spec)
	settle(err)

	if err != nil {
		return swarm.Secret{}, err
	}
	return c.InspectSecret(ctx, id)
}

func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	return c.removing(ctx, cache.EventVolume, name, func() error {
		return c.docker.VolumeRemove(ctx, name, force)
	})
}

// removing makes the removal of the resource key names, attributed.
func (c *Client) removing(
	ctx context.Context,
	typ cache.EventType,
	key string,
	remove func() error,
) error {
	settle := c.expect(ctx, cache.Expectation{Type: typ, Key: key, Action: "remove"})
	err := remove()
	settle(err)

	return err
}

// MapMutator transforms a string map. The caller passes a closure knowing the
// requested patch; the writer hands it the live state read from Docker, which
// is what closes the lost-update window a cache-side merge leaves open.
type MapMutator = func(current map[string]string) (map[string]string, error)

func envSliceToMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		out[k] = v
	}
	return out
}

func (c *Client) UpdateServiceEnv(
	ctx context.Context,
	id string,
	mutate MapMutator,
) (swarm.Service, error) {
	return c.updateContainerSpec(ctx, id, func(cs *swarm.ContainerSpec) error {
		updated, err := mutate(envSliceToMap(cs.Env))
		if err != nil {
			return err
		}
		envSlice := make([]string, 0, len(updated))
		for k, v := range updated {
			envSlice = append(envSlice, k+"="+v)
		}
		sort.Strings(envSlice)
		cs.Env = envSlice
		return nil
	})
}

func (c *Client) UpdateNodeLabels(
	ctx context.Context,
	id string,
	mutate MapMutator,
) (swarm.Node, error) {
	return c.updateNodeSpec(ctx, id, func(spec *swarm.NodeSpec) error {
		updated, err := mutate(spec.Labels)
		if err != nil {
			return err
		}
		spec.Labels = updated
		return nil
	})
}

func (c *Client) UpdateServiceLabels(
	ctx context.Context,
	id string,
	mutate MapMutator,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		updated, err := mutate(spec.Labels)
		if err != nil {
			return err
		}
		spec.Labels = updated
		return nil
	})
}

func (c *Client) UpdateServiceHealthcheck(
	ctx context.Context,
	id string,
	hc *container.HealthConfig,
) (swarm.Service, error) {
	return c.updateContainerSpec(ctx, id, func(cs *swarm.ContainerSpec) error {
		cs.Healthcheck = hc
		return nil
	})
}

func (c *Client) UpdateServiceEndpointMode(
	ctx context.Context,
	id string,
	mode swarm.ResolutionMode,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		if spec.EndpointSpec == nil {
			spec.EndpointSpec = &swarm.EndpointSpec{}
		}
		spec.EndpointSpec.Mode = mode
		return nil
	})
}

// UpdateServiceSpec applies mutate to the service spec as the engine currently
// holds it and writes the result back. A merge patch's base has to be the live
// spec: merging into a cached copy discards whatever was written in between,
// and the engine cannot refuse it, since the version is read with the spec.
func (c *Client) UpdateServiceSpec(
	ctx context.Context,
	id string,
	mutate func(spec *swarm.ServiceSpec) error,
) (swarm.Service, error) {
	return c.updateService(ctx, id, swarm.ServiceUpdateOptions{}, func(svc *swarm.Service) error {
		return mutate(&svc.Spec)
	})
}

func (c *Client) updateService(
	ctx context.Context,
	id string,
	opts swarm.ServiceUpdateOptions,
	mutate func(svc *swarm.Service) error,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}

	if err := checkPinned(ctx, "service", svc.ID, svc.Version); err != nil {
		return swarm.Service{}, err
	}

	if err := mutate(&svc); err != nil {
		return swarm.Service{}, err
	}

	settle := c.expect(ctx, cache.Expectation{
		Type:  cache.EventService,
		Key:   svc.ID,
		After: svc.Version.Index,
	})

	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, opts)
	settle(err)

	if err != nil {
		return swarm.Service{}, err
	}

	return c.InspectService(ctx, id)
}

// updateContainerSpec refuses a service without a container spec rather than
// creating one: these writers edit an existing container, they do not add one.
func (c *Client) updateContainerSpec(
	ctx context.Context,
	id string,
	mutate func(cs *swarm.ContainerSpec) error,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		if spec.TaskTemplate.ContainerSpec == nil {
			return errdefs.InvalidParameter(errors.New("service has no container spec"))
		}
		return mutate(spec.TaskTemplate.ContainerSpec)
	})
}

// ensureContainerSpec is for the reference lists, which a service may gain
// before it has any other container settings.
func ensureContainerSpec(spec *swarm.ServiceSpec) *swarm.ContainerSpec {
	if spec.TaskTemplate.ContainerSpec == nil {
		spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{}
	}
	return spec.TaskTemplate.ContainerSpec
}

func (c *Client) UpdateServiceResources(
	ctx context.Context,
	id string,
	resources *swarm.ResourceRequirements,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.Resources = resources
		return nil
	})
}

func (c *Client) UpdateServicePlacement(
	ctx context.Context,
	id string,
	placement *swarm.Placement,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.Placement = placement
		return nil
	})
}

func (c *Client) UpdateServicePorts(
	ctx context.Context,
	id string,
	ports []swarm.PortConfig,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		if spec.EndpointSpec == nil {
			spec.EndpointSpec = &swarm.EndpointSpec{}
		}
		spec.EndpointSpec.Ports = ports
		return nil
	})
}

func (c *Client) UpdateServiceUpdatePolicy(
	ctx context.Context,
	id string,
	policy *swarm.UpdateConfig,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		spec.UpdateConfig = policy
		return nil
	})
}

func (c *Client) UpdateServiceRollbackPolicy(
	ctx context.Context,
	id string,
	policy *swarm.UpdateConfig,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		spec.RollbackConfig = policy
		return nil
	})
}

func (c *Client) UpdateServiceLogDriver(
	ctx context.Context,
	id string,
	driver *swarm.Driver,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.LogDriver = driver
		return nil
	})
}

func (c *Client) UpdateServiceConfigs(
	ctx context.Context,
	id string,
	configs []*swarm.ConfigReference,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		ensureContainerSpec(spec).Configs = configs
		return nil
	})
}

func (c *Client) UpdateServiceSecrets(
	ctx context.Context,
	id string,
	secrets []*swarm.SecretReference,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		ensureContainerSpec(spec).Secrets = secrets
		return nil
	})
}

func (c *Client) UpdateServiceNetworks(
	ctx context.Context,
	id string,
	networks []swarm.NetworkAttachmentConfig,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.Networks = networks
		return nil
	})
}

func (c *Client) UpdateServiceMounts(
	ctx context.Context,
	id string,
	mounts []mount.Mount,
) (swarm.Service, error) {
	return c.UpdateServiceSpec(ctx, id, func(spec *swarm.ServiceSpec) error {
		ensureContainerSpec(spec).Mounts = mounts
		return nil
	})
}

func (c *Client) UpdateServiceContainerConfig(
	ctx context.Context,
	id string,
	apply func(spec *swarm.ContainerSpec),
) (swarm.Service, error) {
	return c.updateContainerSpec(ctx, id, func(cs *swarm.ContainerSpec) error {
		apply(cs)
		return nil
	})
}

// swarmResources are the lists only a manager answers. A worker still lists
// its own networks and volumes, so a sync that reached one is not a success.
var swarmResources = []string{"nodes", "services", "tasks", "configs", "secrets"}

// syncedResources is every list a full sync reads, in the order its error names them.
var syncedResources = []string{
	"nodes", "services", "tasks", "configs", "secrets", "networks", "volumes",
}

// fullSyncError decides whether a sync with these resources failed counts as
// a failure: every one of them, or every swarm resource. The error wraps each
// list's own, so a caller can tell a timeout from a refusal.
func fullSyncError(failed map[string]error) error {
	var summary string

	switch {
	case len(failed) == len(syncedResources):
		summary = "all resource syncs failed"
	case !slices.ContainsFunc(swarmResources, func(name string) bool { return failed[name] == nil }):
		// A manager whose swarm has lost quorum refuses the same lists a worker does.
		summary = "no swarm resource could be listed: docker.host must point at a swarm manager, " +
			"and the swarm must have a leader"
	default:
		return nil
	}

	causes := make([]error, 0, len(failed))
	for _, name := range syncedResources {
		if err := failed[name]; err != nil {
			causes = append(causes, fmt.Errorf("%s: %w", name, err))
			summary += "; " + causes[len(causes)-1].Error()
		}
	}

	return &fullSyncFailure{summary, causes}
}

// fullSyncFailure keeps its causes on one line, where errors.Join would not.
type fullSyncFailure struct {
	message string
	causes  []error
}

func (e *fullSyncFailure) Error() string   { return e.message }
func (e *fullSyncFailure) Unwrap() []error { return e.causes }
