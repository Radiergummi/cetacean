package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/core"
	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/filter"
	"github.com/radiergummi/cetacean/internal/prometheus"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

const defaultLogLimit = 500
const maxLogLimit = 10000
const maxLogSSEConns = 128

// maxPreferWaits bounds writes held open by Prefer: wait, each up to minutes.
const maxPreferWaits = 32

// logResumeTail bounds how far back a resumed SSE stream reaches for the lines
// it missed while the client was disconnected. A client away for longer than
// this loses the overflow, which beats replaying an entire log.
const logResumeTail = 500

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

// Narrow write interfaces: each handler depends only on the methods it needs,
// and docker.Client satisfies all of them structurally. Service operations are
// grouped into lifecycle and spec updates, so a test can mock only the group
// it exercises.

type ServiceLifecycleWriter interface {
	ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
	UpdateServiceImage(ctx context.Context, id string, image string) (swarm.Service, error)
	RollbackService(ctx context.Context, id string) (swarm.Service, error)
	RestartService(ctx context.Context, id string) (swarm.Service, error)
	RemoveService(ctx context.Context, id string) error
}

type ServiceSpecWriter interface {
	// UpdateServiceSpec is how every merge patch writes: the base it merges
	// into must be the live spec, so the merge runs inside the writer rather
	// than against the asynchronously filled cache (M-42).
	UpdateServiceSpec(
		ctx context.Context,
		id string,
		mutate func(spec *swarm.ServiceSpec) error,
	) (swarm.Service, error)
}

// ServiceWriter composes all service write interfaces.
type ServiceWriter interface {
	ServiceLifecycleWriter
	ServiceSpecWriter
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
	InspectConfig(ctx context.Context, id string) (swarm.Config, error)
	RemoveConfig(ctx context.Context, id string) error
	UpdateConfigLabels(
		ctx context.Context,
		id string,
		mutate func(current map[string]string) (map[string]string, error),
	) (swarm.Config, error)
}

type SecretWriter interface {
	CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error)
	InspectSecret(ctx context.Context, id string) (swarm.Secret, error)
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
	PluginInstall(
		ctx context.Context,
		remote string,
		approved types.PluginPrivileges,
	) (*types.Plugin, error)
	PluginUpgrade(ctx context.Context, name, remote string, approved types.PluginPrivileges) error
	PluginPrivileges(ctx context.Context, remote string) (types.PluginPrivileges, error)
	PluginConfigure(ctx context.Context, name string, args []string) error
}

type Handlers struct {
	cache                *cache.Cache
	broadcaster          *sse.Broadcaster
	dockerClient         DockerLogStreamer
	systemClient         DockerSystemClient
	serviceLifecycle     ServiceLifecycleWriter
	writes               *cluster.Writes
	serviceSpec          ServiceSpecWriter
	nodeWriter           NodeWriter
	configWriter         ConfigWriter
	secretWriter         SecretWriter
	resourceRemover      ResourceRemover
	pluginClient         DockerPluginClient
	ready                <-chan struct{}
	liveness             LivenessReporter
	promClient           *prometheus.Client
	operationsLevel      config.OperationsLevel
	tokenOperationsLevel config.OperationsLevel
	recEngine            *recommendations.Engine
	acl                  *acl.Evaluator
	refresher            ResourceRefresher
	localNodeMu          sync.Mutex
	localNodeID          string
	localNodeRetryAfter  *time.Time

	logStreams         sse.Slots
	streams            *sse.Lifetime // ends the metrics stream and log follows at shutdown
	logReads           *sse.Slots    // see logs.MaxConcurrentReads
	secretEntries      *cluster.SecretEntries
	activePreferWaits  atomic.Int64
	metricsStreamCount atomic.Int64
	tickerInterval     time.Duration // override for tick interval in tests; zero means use step duration
	dockerVersionCache *dockerVersionCache
	diskUsage          diskUsageCache
	imageDigests       ImageDigestSource // nil when server.image_check is off
	attestations       cluster.AttestationSource

	// topologyDocs and stackDocs memoise rendered documents that are pure
	// functions of the cache generation and what the caller may see.
	topologyDocs *projectionCache
	stackDocs    *projectionCache

	versions *cluster.VersionIndex
}

func NewHandlers(
	shared *core.Core,
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
		cache:            shared.Cache,
		broadcaster:      b,
		dockerClient:     dc,
		systemClient:     sc,
		serviceLifecycle: wc,
		writes:           cluster.NewWrites(shared.Cache, wc),
		serviceSpec:      wc,
		nodeWriter:       wc,
		configWriter:     wc,
		secretWriter:     wc,
		resourceRemover:  wc,
		pluginClient:     pc,
		ready:            ready,
		promClient:       promClient,
		operationsLevel:  operationsLevel,

		// Unset, a token is the person it speaks for. Only a deployment that
		// says otherwise holds it lower.
		tokenOperationsLevel: operationsLevel,
		recEngine:            recEngine,
		acl:                  aclEval,
		dockerVersionCache:   newDockerVersionCache(),
		topologyDocs:         newProjectionCache(),
		stackDocs:            newProjectionCache(),
		versions:             shared.Versions,
		logReads:             shared.LogReads,
		secretEntries:        shared.SecretEntries,
		streams:              shared.Streams,
	}
}

// SetTokenOperationsLevel holds a token-authenticated caller below the tier the
// deployment runs at. Already narrowed by the global level when it arrives.
func (h *Handlers) SetTokenOperationsLevel(level config.OperationsLevel) {
	h.tokenOperationsLevel = level
}

// ImageDigestSource is the registry image check as the handlers read it.
type ImageDigestSource interface {
	cluster.ImageDigests
	Generation() uint64
}

// SetImageDigests reports a newer image for a service's tag from d.
func (h *Handlers) SetImageDigests(d ImageDigestSource) {
	h.imageDigests = d
}

// SetAttestations answers the provenance and SBOM endpoints from s.
func (h *Handlers) SetAttestations(s cluster.AttestationSource) {
	h.attestations = s
}

// imageUpdate is what svc's response says about its image tag, nil when the
// check is off.
func (h *Handlers) imageUpdate(svc swarm.Service) *cluster.ImageUpdate {
	if h.imageDigests == nil {
		return nil
	}

	return cluster.ServiceImageUpdate(svc, h.imageDigests)
}

// requireAnyGrant checks that the identity has at least one grant.
// Used to gate cluster-wide endpoints when ACL is active.
func (h *Handlers) requireAnyGrant(w http.ResponseWriter, r *http.Request) bool {
	// Everything behind this gate is ACL-filtered — topology, search, history,
	// recommendations, the stack summary — and none of them report an Allow to
	// carry the marker for them.
	varyByIdentity(w)

	id := auth.IdentityFromContext(r.Context())
	if h.acl.HasAnyGrant(id) {
		return true
	}
	problem.Write(w, r, "ACL001", "no grants found for this identity")
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

// requireReadEverything gates the Prometheus proxy: PromQL reaches every
// service and node, so nothing narrower than read on a bare "*" may query it.
func (h *Handlers) requireReadEverything(w http.ResponseWriter, r *http.Request) bool {
	varyByIdentity(w)

	if h.acl.CanReadEverything(auth.IdentityFromContext(r.Context())) {
		return true
	}
	problem.Write(w, r, "ACL001", `metrics require read access on "*"`)
	return false
}

// withReadEverything wraps a handler with a requireReadEverything check.
func (h *Handlers) withReadEverything(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.requireReadEverything(w, r) {
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
	// In place, for the reason exprFilter gives.
	filtered := items[:0]
	for _, item := range items {
		if cluster.ContainsFold(name(item), q) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

const maxFilterLen = 512

// filterBudget bounds one request's filter evaluation across every item.
const filterBudget = 250 * time.Millisecond

func exprFilter[T any](
	items []T,
	expr string,
	env filter.Env[T],
	w http.ResponseWriter,
	r *http.Request,
) ([]T, bool) {
	if expr == "" {
		return items, true
	}
	if len(expr) > maxFilterLen {
		problem.Write(w, r, "FLT001", "filter expression too long")
		return nil, false
	}
	prog, err := filter.Compile(expr, env)
	if err != nil {
		problem.Write(w, r, "FLT002", fmt.Sprintf("invalid filter expression: %s", err))
		return nil, false
	}
	// Filtered in place. prepareList is the only caller and items is the copy
	// the cache handed it, so nothing else can observe the reordering; growing
	// a second slice would copy every surviving item again.
	filtered := items[:0]
	var m map[string]any
	ctx, cancel := context.WithTimeout(r.Context(), filterBudget)
	defer cancel()
	for _, item := range items {
		m = env.Build(item, m)
		ok, err := filter.EvaluateContext(ctx, prog, m)
		if errors.Is(err, filter.ErrBudget) {
			problem.Write(
				w,
				r,
				"FLT004",
				"filter expression too expensive to evaluate over this list",
			)
			return nil, false
		}
		if err != nil {
			problem.Write(w, r, "FLT003", fmt.Sprintf("filter evaluation error: %s", err))
			return nil, false
		}
		if ok {
			filtered = append(filtered, item)
		}
	}
	return filtered, true
}

// tryAcquire takes one of max slots on counter, or reports that none is free.
// The caller releases a slot it took with counter.Add(-1).
func tryAcquire(counter *atomic.Int64, max int64) bool {
	for {
		current := counter.Load()
		if current >= max {
			return false
		}
		if counter.CompareAndSwap(current, current+1) {
			return true
		}
	}
}
