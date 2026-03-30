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

	"github.com/radiergummi/cetacean/internal/api/prometheus"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/filter"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

const defaultLogLimit = 500
const maxLogLimit = 10000
const maxLogSSEConns = 128

var activeLogSSEConns atomic.Int64

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

type DockerWriteClient interface {
	ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
	UpdateServiceImage(ctx context.Context, id string, image string) (swarm.Service, error)
	RollbackService(ctx context.Context, id string) (swarm.Service, error)
	RestartService(ctx context.Context, id string) (swarm.Service, error)
	UpdateNodeAvailability(
		ctx context.Context,
		id string,
		availability swarm.NodeAvailability,
	) (swarm.Node, error)
	RemoveTask(ctx context.Context, id string) error
	RemoveService(ctx context.Context, id string) error
	UpdateServiceEnv(ctx context.Context, id string, env map[string]string) (swarm.Service, error)
	UpdateNodeLabels(ctx context.Context, id string, labels map[string]string) (swarm.Node, error)
	UpdateNodeRole(ctx context.Context, id string, role swarm.NodeRole) (swarm.Node, error)
	RemoveNode(ctx context.Context, id string, force bool) error
	RemoveNetwork(ctx context.Context, id string) error
	RemoveConfig(ctx context.Context, id string) error
	RemoveSecret(ctx context.Context, id string) error
	RemoveVolume(ctx context.Context, name string, force bool) error
	CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error)
	CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error)
	UpdateConfigLabels(
		ctx context.Context,
		id string,
		labels map[string]string,
	) (swarm.Config, error)
	UpdateSecretLabels(
		ctx context.Context,
		id string,
		labels map[string]string,
	) (swarm.Secret, error)
	UpdateServiceLabels(
		ctx context.Context,
		id string,
		labels map[string]string,
	) (swarm.Service, error)
	UpdateServiceResources(
		ctx context.Context,
		id string,
		resources *swarm.ResourceRequirements,
	) (swarm.Service, error)
	UpdateServiceMode(
		ctx context.Context,
		id string,
		mode swarm.ServiceMode,
	) (swarm.Service, error)
	UpdateServiceEndpointMode(
		ctx context.Context,
		id string,
		mode swarm.ResolutionMode,
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
	writeClient         DockerWriteClient
	pluginClient        DockerPluginClient
	ready               <-chan struct{}
	promClient          *prometheus.Client
	operationsLevel     config.OperationsLevel
	recEngine           *recommendations.Engine
	localNodeMu         sync.Mutex
	localNodeID         string
	localNodeDone       bool
	localNodeRetryAfter *time.Time
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
) *Handlers {
	return &Handlers{
		cache:           c,
		broadcaster:     b,
		dockerClient:    dc,
		systemClient:    sc,
		writeClient:     wc,
		pluginClient:    pc,
		ready:           ready,
		promClient:      promClient,
		operationsLevel: operationsLevel,
		recEngine:       recEngine,
	}
}

func searchFilter[T any](items []T, query string, name func(T) string) []T {
	if query == "" {
		return items
	}
	q := strings.ToLower(query)
	var filtered []T
	for _, item := range items {
		if containsFold(name(item), q) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// containsFold reports whether s contains substr using case-insensitive
// comparison, or whether the query matches segment prefixes of s.
// substr must already be lowercased.
func containsFold(s, substrLower string) bool {
	if containsFoldNoAlloc(s, substrLower) {
		return true
	}

	// Segment-prefix matching requires lowercased input; only allocate if
	// the string actually contains separators (otherwise segmentPrefixMatch
	// returns false for single-segment targets anyway).
	if !strings.ContainsAny(s, "_-") {
		return false
	}

	return segmentPrefixMatch(strings.ToLower(s), substrLower)
}

// containsFoldNoAlloc reports whether s contains substr (which must be
// lowercased) using case-insensitive comparison without allocating.
// Only handles ASCII case folding; non-ASCII letters are compared as-is.
func containsFoldNoAlloc(s, substrLower string) bool {
	if len(substrLower) == 0 {
		return true
	}

	if len(substrLower) > len(s) {
		return false
	}

	for i := 0; i <= len(s)-len(substrLower); i++ {
		match := true

		for j := 0; j < len(substrLower); j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}

			if c != substrLower[j] {
				match = false
				break
			}
		}

		if match {
			return true
		}
	}

	return false
}

var separatorReplacer = strings.NewReplacer("_", "", "-", "")

func isSeparator(r rune) bool { return r == '_' || r == '-' }

// segmentPrefixMatch checks if query matches target using segment-prefix
// matching. The target is split by '_' and '-' into segments, and each group
// of query characters must match the prefix of a segment, in order, with
// segments skippable. Uses memoized backtracking for ambiguous boundaries.
//
// Both arguments must already be lowercased.
func segmentPrefixMatch(targetLower, queryLower string) bool {
	if len(queryLower) == 0 {
		return true
	}

	// Strip separators from query (user may type "go_gc" meaning "go" + "gc")
	query := separatorReplacer.Replace(queryLower)
	if len(query) == 0 {
		return true
	}

	segments := strings.FieldsFunc(targetLower, isSeparator)

	// Single-segment targets are already covered by substring match in containsFold
	if len(segments) <= 1 {
		return false
	}

	type key struct{ qi, si int }
	memo := map[key]bool{}

	var match func(qi, si int) bool
	match = func(qi, si int) bool {
		if qi >= len(query) {
			return true
		}

		if si >= len(segments) {
			return false
		}

		k := key{qi, si}
		if v, ok := memo[k]; ok {
			return v
		}

		result := false
		for s := si; s < len(segments) && !result; s++ {
			seg := segments[s]
			maxMatch := 0

			for maxMatch < len(seg) && qi+maxMatch < len(query) && query[qi+maxMatch] == seg[maxMatch] {
				maxMatch++
			}

			for take := maxMatch; take >= 1 && !result; take-- {
				if match(qi+take, s+1) {
					result = true
				}
			}
		}

		memo[k] = result
		return result
	}

	return match(0, 0)
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
