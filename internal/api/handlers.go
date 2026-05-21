package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api/prometheus"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/filter"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

const defaultLogLimit = 500
const maxLogLimit = 10000
const maxLogSSEConns = 128

type DockerLogStreamer interface {
	Logs(
		ctx context.Context,
		kind docker.LogKind,
		id string,
		tail string,
		follow bool,
		since, until string,
	) (io.ReadCloser, error)
}

type DockerSystemClient interface {
	SwarmInspect(ctx context.Context) (swarm.Swarm, error)
	DiskUsage(ctx context.Context) (types.DiskUsage, error)
	LocalNodeID(ctx context.Context) (string, error)
	UpdateSwarm(
		ctx context.Context,
		spec swarm.Spec,
		version swarm.Version,
		flags swarm.UpdateFlags,
	) error
	GetUnlockKey(ctx context.Context) (string, error)
	UnlockSwarm(ctx context.Context, key string) error
}

// Narrow write interfaces. Each handler depends only on the methods it
// needs. The concrete docker.Client satisfies all of them via Go's
// structural typing.
//
// Service operations are split into logical groups: lifecycle (scale,
// image, rollback, restart, remove), spec updates (env, labels,
// resources, mode, ports, placement, healthcheck, policies, log driver),
// and attachment updates (configs, secrets, networks, mounts, container
// config). Tests can mock just the group they exercise.

type ServiceLifecycleWriter interface {
	ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
	UpdateServiceImage(ctx context.Context, id string, image string) (swarm.Service, error)
	RollbackService(ctx context.Context, id string) (swarm.Service, error)
	RestartService(ctx context.Context, id string) (swarm.Service, error)
	RemoveService(ctx context.Context, id string) error
	UpdateServiceMode(ctx context.Context, id string, mode swarm.ServiceMode) (swarm.Service, error)
	UpdateServiceEndpointMode(
		ctx context.Context,
		id string,
		mode swarm.ResolutionMode,
	) (swarm.Service, error)
}

type ServiceSpecWriter interface {
	UpdateServiceEnv(
		ctx context.Context,
		id string,
		mutate func(current map[string]string) (map[string]string, error),
	) (swarm.Service, error)
	UpdateServiceLabels(
		ctx context.Context,
		id string,
		mutate func(current map[string]string) (map[string]string, error),
	) (swarm.Service, error)
	UpdateServiceResources(
		ctx context.Context,
		id string,
		resources *swarm.ResourceRequirements,
	) (swarm.Service, error)
	UpdateServiceHealthcheck(
		ctx context.Context,
		id string,
		hc *container.HealthConfig,
	) (swarm.Service, error)
	UpdateServicePlacement(
		ctx context.Context,
		id string,
		placement *swarm.Placement,
	) (swarm.Service, error)
	UpdateServicePorts(
		ctx context.Context,
		id string,
		ports []swarm.PortConfig,
	) (swarm.Service, error)
	UpdateServiceUpdatePolicy(
		ctx context.Context,
		id string,
		policy *swarm.UpdateConfig,
	) (swarm.Service, error)
	UpdateServiceRollbackPolicy(
		ctx context.Context,
		id string,
		policy *swarm.UpdateConfig,
	) (swarm.Service, error)
	UpdateServiceLogDriver(
		ctx context.Context,
		id string,
		driver *swarm.Driver,
	) (swarm.Service, error)
}

type ServiceAttachmentWriter interface {
	UpdateServiceConfigs(
		ctx context.Context,
		id string,
		configs []*swarm.ConfigReference,
	) (swarm.Service, error)
	UpdateServiceSecrets(
		ctx context.Context,
		id string,
		secrets []*swarm.SecretReference,
	) (swarm.Service, error)
	UpdateServiceNetworks(
		ctx context.Context,
		id string,
		networks []swarm.NetworkAttachmentConfig,
	) (swarm.Service, error)
	UpdateServiceMounts(ctx context.Context, id string, mounts []mount.Mount) (swarm.Service, error)
	UpdateServiceContainerConfig(
		ctx context.Context,
		id string,
		apply func(spec *swarm.ContainerSpec),
	) (swarm.Service, error)
}

// ServiceWriter composes all service write interfaces.
type ServiceWriter interface {
	ServiceLifecycleWriter
	ServiceSpecWriter
	ServiceAttachmentWriter
}

type NodeWriter interface {
	UpdateNodeAvailability(
		ctx context.Context,
		id string,
		availability swarm.NodeAvailability,
	) (swarm.Node, error)
	UpdateNodeLabels(
		ctx context.Context,
		id string,
		mutate func(current map[string]string) (map[string]string, error),
	) (swarm.Node, error)
	UpdateNodeRole(ctx context.Context, id string, role swarm.NodeRole) (swarm.Node, error)
	RemoveNode(ctx context.Context, id string, force bool) error
}

type ConfigWriter interface {
	CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error)
	RemoveConfig(ctx context.Context, id string) error
	UpdateConfigLabels(
		ctx context.Context,
		id string,
		mutate func(current map[string]string) (map[string]string, error),
	) (swarm.Config, error)
}

type SecretWriter interface {
	CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error)
	RemoveSecret(ctx context.Context, id string) error
	UpdateSecretLabels(
		ctx context.Context,
		id string,
		mutate func(current map[string]string) (map[string]string, error),
	) (swarm.Secret, error)
}

type ResourceRemover interface {
	RemoveTask(ctx context.Context, id string) error
	RemoveNetwork(ctx context.Context, id string) error
	RemoveVolume(ctx context.Context, name string, force bool) error
}

// DockerWriteClient composes all resource-specific write interfaces.
// Tests can mock narrower interfaces; the concrete docker.Client satisfies this.
type DockerWriteClient interface {
	ServiceWriter
	NodeWriter
	ConfigWriter
	SecretWriter
	ResourceRemover
}

type DockerPluginClient interface {
	PluginList(ctx context.Context) (types.PluginsListResponse, error)
	PluginInspect(ctx context.Context, name string) (*types.Plugin, error)
	PluginEnable(ctx context.Context, name string) error
	PluginDisable(ctx context.Context, name string) error
	PluginRemove(ctx context.Context, name string, force bool) error
	PluginInstall(ctx context.Context, remote string) (*types.Plugin, error)
	PluginUpgrade(ctx context.Context, name string, remote string) error
	PluginPrivileges(ctx context.Context, remote string) (types.PluginPrivileges, error)
	PluginConfigure(ctx context.Context, name string, args []string) error
}

type Handlers struct {
	cache               *cache.Cache
	broadcaster         *sse.Broadcaster
	dockerClient        DockerLogStreamer
	systemClient        DockerSystemClient
	serviceLifecycle    ServiceLifecycleWriter
	serviceSpec         ServiceSpecWriter
	serviceAttachment   ServiceAttachmentWriter
	nodeWriter          NodeWriter
	configWriter        ConfigWriter
	secretWriter        SecretWriter
	resourceRemover     ResourceRemover
	pluginClient        DockerPluginClient
	ready               <-chan struct{}
	promClient          *prometheus.Client
	operationsLevel     config.OperationsLevel
	recEngine           *recommendations.Engine
	acl                 *acl.Evaluator
	localNodeMu         sync.Mutex
	localNodeID         string
	localNodeDone       bool
	localNodeRetryAfter *time.Time

	activeLogSSEConns  atomic.Int64
	metricsStreamCount atomic.Int32
	tickerInterval     time.Duration // override for tick interval in tests; zero means use step duration
	dockerVersionCache *dockerVersionCache
}

func NewHandlers(
	c *cache.Cache,
	b *sse.Broadcaster,
	dc DockerLogStreamer,
	sc DockerSystemClient,
	wc DockerWriteClient,
	pc DockerPluginClient,
	ready <-chan struct{},
	promClient *prometheus.Client,
	operationsLevel config.OperationsLevel,
	recEngine *recommendations.Engine,
	aclEval *acl.Evaluator,
) *Handlers {
	return &Handlers{
		cache:              c,
		broadcaster:        b,
		dockerClient:       dc,
		systemClient:       sc,
		serviceLifecycle:   wc,
		serviceSpec:        wc,
		serviceAttachment:  wc,
		nodeWriter:         wc,
		configWriter:       wc,
		secretWriter:       wc,
		resourceRemover:    wc,
		pluginClient:       pc,
		ready:              ready,
		promClient:         promClient,
		operationsLevel:    operationsLevel,
		recEngine:          recEngine,
		acl:                aclEval,
		dockerVersionCache: newDockerVersionCache(),
	}
}

// requireAnyGrant checks that the identity has at least one grant.
// Used to gate cluster-wide endpoints when ACL is active.
func (h *Handlers) requireAnyGrant(w http.ResponseWriter, r *http.Request) bool {
	id := auth.IdentityFromContext(r.Context())
	if h.acl.HasAnyGrant(id) {
		return true
	}
	writeErrorCode(w, r, "ACL001", "no grants found for this identity")
	return false
}

// withAnyGrant wraps a handler with a requireAnyGrant check.
func (h *Handlers) withAnyGrant(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.requireAnyGrant(w, r) {
			return
		}
		next(w, r)
	}
}

func searchFilter[T any](items []T, query string, name func(T) string) []T {
	if query == "" {
		return items
	}
	q := strings.ToLower(query)
	var filtered []T
	for _, item := range items {
		if cluster.ContainsFold(name(item), q) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

const maxFilterLen = 512

func exprFilter[T any](
	items []T,
	expr string,
	env func(T, map[string]any) map[string]any,
	w http.ResponseWriter,
	r *http.Request,
) ([]T, bool) {
	if expr == "" {
		return items, true
	}
	if len(expr) > maxFilterLen {
		writeErrorCode(w, r, "FLT001", "filter expression too long")
		return nil, false
	}
	prog, err := filter.Compile(expr)
	if err != nil {
		writeErrorCode(w, r, "FLT002", fmt.Sprintf("invalid filter expression: %s", err))
		return nil, false
	}
	var filtered []T
	var m map[string]any
	for _, item := range items {
		m = env(item, m)
		ok, err := filter.Evaluate(prog, m)
		if err != nil {
			writeErrorCode(w, r, "FLT003", fmt.Sprintf("filter evaluation error: %s", err))
			return nil, false
		}
		if ok {
			filtered = append(filtered, item)
		}
	}
	return filtered, true
}
