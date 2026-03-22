package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/filter"
	"github.com/radiergummi/cetacean/internal/version"
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
	PluginList(ctx context.Context) (types.PluginsListResponse, error)
	LocalNodeID(ctx context.Context) (string, error)
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
	RemoveNode(ctx context.Context, id string) error
	RemoveNetwork(ctx context.Context, id string) error
	RemoveConfig(ctx context.Context, id string) error
	RemoveSecret(ctx context.Context, id string) error
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
	UpdateServiceContainerConfig(ctx context.Context, id string, apply func(spec *swarm.ContainerSpec)) (swarm.Service, error)
}

type Handlers struct {
	cache               *cache.Cache
	broadcaster         *Broadcaster
	dockerClient        DockerLogStreamer
	systemClient        DockerSystemClient
	writeClient         DockerWriteClient
	ready               <-chan struct{}
	promClient          *PromClient
	operationsLevel     config.OperationsLevel
	localNodeMu         sync.Mutex
	localNodeID         string
	localNodeDone       bool
	localNodeRetryAfter *time.Time
}

func NewHandlers(
	c *cache.Cache,
	b *Broadcaster,
	dc DockerLogStreamer,
	sc DockerSystemClient,
	wc DockerWriteClient,
	ready <-chan struct{},
	promClient *PromClient,
	operationsLevel config.OperationsLevel,
) *Handlers {
	return &Handlers{
		cache:           c,
		broadcaster:     b,
		dockerClient:    dc,
		systemClient:    sc,
		writeClient:     wc,
		ready:           ready,
		promClient:      promClient,
		operationsLevel: operationsLevel,
	}
}

func (h *Handlers) streamList(w http.ResponseWriter, r *http.Request, typ string) {
	h.broadcaster.serveSSE(w, r, typeMatcher(typ))
}

func (h *Handlers) streamResource(w http.ResponseWriter, r *http.Request, typ, id string) {
	h.broadcaster.serveSSE(w, r, resourceMatcher(typ, id))
}

func (h *Handlers) isReady() bool {
	select {
	case <-h.ready:
		return true
	default:
		return false
	}
}

func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"status":          "ok",
		"version":         version.Version,
		"commit":          version.Commit,
		"buildDate":       version.Date,
		"operationsLevel": h.operationsLevel,
	})
}

func (h *Handlers) HandleReady(w http.ResponseWriter, r *http.Request) {
	if !h.isReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, map[string]string{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, v any) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// HandleProfile returns the authenticated user's identity as JSON.
// Registered with content negotiation so /profile serves the SPA for
// browsers and JSON for API clients (/profile.json or Accept: application/json).
func HandleProfile(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFromContext(r.Context())
	if id == nil {
		writeProblem(w, r, http.StatusUnauthorized, "not authenticated")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSONWithETag(w, r, id)
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
	lower := strings.ToLower(s)

	if strings.Contains(lower, substrLower) {
		return true
	}

	return segmentPrefixMatch(lower, substrLower)
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
		writeProblemTyped(w, r, ProblemDetail{
			Type:   "urn:cetacean:error:filter-invalid",
			Title:  "Invalid Filter Expression",
			Status: http.StatusBadRequest,
			Detail: "filter expression too long",
		})
		return nil, false
	}
	prog, err := filter.Compile(expr)
	if err != nil {
		writeProblemTyped(w, r, ProblemDetail{
			Type:   "urn:cetacean:error:filter-invalid",
			Title:  "Invalid Filter Expression",
			Status: http.StatusBadRequest,
			Detail: fmt.Sprintf("invalid filter expression: %s", err),
		})
		return nil, false
	}
	var filtered []T
	var m map[string]any
	for _, item := range items {
		m = env(item, m)
		ok, err := filter.Evaluate(prog, m)
		if err != nil {
			writeProblemTyped(w, r, ProblemDetail{
				Type:   "urn:cetacean:error:filter-invalid",
				Title:  "Invalid Filter Expression",
				Status: http.StatusBadRequest,
				Detail: fmt.Sprintf("filter evaluation error: %s", err),
			})
			return nil, false
		}
		if ok {
			filtered = append(filtered, item)
		}
	}
	return filtered, true
}

func (h *Handlers) getLocalNodeID() string {
	h.localNodeMu.Lock()
	defer h.localNodeMu.Unlock()
	if h.localNodeDone {
		return h.localNodeID
	}

	// Avoid hammering Docker API on every request when it's failing:
	// back off for 30s after a failed attempt.
	if h.localNodeRetryAfter != nil && time.Now().Before(*h.localNodeRetryAfter) {
		return h.localNodeID
	}

	if h.systemClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		id, err := h.systemClient.LocalNodeID(ctx)
		if err != nil {
			slog.Warn("failed to get local node ID", "error", err)
			retryAt := time.Now().Add(30 * time.Second)
			h.localNodeRetryAfter = &retryAt
		} else if id != "" {
			h.localNodeID = id
			h.localNodeDone = true
			h.localNodeRetryAfter = nil
		}
	}
	return h.localNodeID
}

func (h *Handlers) HandleCluster(w http.ResponseWriter, r *http.Request) {
	snap := h.cache.Snapshot()
	extra := map[string]any{
		"nodeCount":            snap.NodeCount,
		"serviceCount":         snap.ServiceCount,
		"taskCount":            snap.TaskCount,
		"stackCount":           snap.StackCount,
		"tasksByState":         snap.TasksByState,
		"nodesReady":           snap.NodesReady,
		"nodesDown":            snap.NodesDown,
		"nodesDraining":        snap.NodesDraining,
		"servicesConverged":    snap.ServicesConverged,
		"servicesDegraded":     snap.ServicesDegraded,
		"reservedCPU":          snap.ReservedCPU,
		"reservedMemory":       snap.ReservedMemory,
		"totalCPU":             snap.TotalCPU,
		"totalMemory":          snap.TotalMemory,
		"prometheusConfigured": h.promClient != nil,
	}
	if id := h.getLocalNodeID(); id != "" {
		extra["localNodeID"] = id
	}
	writeJSONWithETag(w, r, NewDetailResponse("/cluster", "Cluster", extra))
}

type ClusterMetrics struct {
	CPU    ResourceMetric `json:"cpu"`
	Memory ResourceMetric `json:"memory"`
	Disk   ResourceMetric `json:"disk"`
}

type ResourceMetric struct {
	Used    float64 `json:"used"`
	Total   float64 `json:"total"`
	Percent float64 `json:"percent"`
}

func (h *Handlers) HandleClusterCapacity(w http.ResponseWriter, r *http.Request) {
	snap := h.cache.Snapshot()
	extra := map[string]any{
		"maxNodeCPU":    snap.MaxNodeCPU,
		"maxNodeMemory": snap.MaxNodeMemory,
		"totalCPU":      snap.TotalCPU,
		"totalMemory":   snap.TotalMemory,
		"nodeCount":     snap.NodeCount,
	}
	writeJSONWithETag(w, r, NewDetailResponse("/cluster/capacity", "ClusterCapacity", extra))
}

func (h *Handlers) HandleClusterMetrics(w http.ResponseWriter, r *http.Request) {
	if h.promClient == nil {
		writeProblem(w, r, http.StatusNotFound, "prometheus not configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	snap := h.cache.Snapshot()

	var metrics ClusterMetrics
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(3)

	// CPU utilization
	go func() {
		defer wg.Done()
		results, err := h.promClient.InstantQuery(
			ctx,
			`sum(rate(node_cpu_seconds_total{mode!="idle"}[5m])) / sum(rate(node_cpu_seconds_total[5m])) * 100`,
		)
		if err != nil {
			slog.Warn("cluster metrics: CPU query failed", "error", err)
			return
		}
		if len(results) > 0 {
			mu.Lock()
			metrics.CPU = ResourceMetric{
				Used:    float64(snap.TotalCPU) * results[0].Value / 100,
				Total:   float64(snap.TotalCPU),
				Percent: results[0].Value,
			}
			mu.Unlock()
		}
	}()

	// Memory utilization
	go func() {
		defer wg.Done()
		results, err := h.promClient.InstantQuery(ctx,
			`sum(node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes)`)
		if err != nil {
			slog.Warn("cluster metrics: memory query failed", "error", err)
			return
		}
		if len(results) > 0 {
			mu.Lock()
			total := float64(snap.TotalMemory)
			used := results[0].Value
			pct := 0.0
			if total > 0 {
				pct = used / total * 100
			}
			metrics.Memory = ResourceMetric{Used: used, Total: total, Percent: pct}
			mu.Unlock()
		}
	}()

	// Disk utilization
	go func() {
		defer wg.Done()
		type pair struct{ total, avail float64 }
		var p pair
		var pmu sync.Mutex
		var dwg sync.WaitGroup
		dwg.Add(2)
		go func() {
			defer dwg.Done()
			r, err := h.promClient.InstantQuery(
				ctx,
				`sum(node_filesystem_size_bytes{mountpoint="/"})`,
			)
			if err == nil && len(r) > 0 {
				pmu.Lock()
				p.total = r[0].Value
				pmu.Unlock()
			}
		}()
		go func() {
			defer dwg.Done()
			r, err := h.promClient.InstantQuery(
				ctx,
				`sum(node_filesystem_avail_bytes{mountpoint="/"})`,
			)
			if err == nil && len(r) > 0 {
				pmu.Lock()
				p.avail = r[0].Value
				pmu.Unlock()
			}
		}()
		dwg.Wait()

		if p.total > 0 {
			used := p.total - p.avail
			mu.Lock()
			metrics.Disk = ResourceMetric{
				Used:    used,
				Total:   p.total,
				Percent: used / p.total * 100,
			}
			mu.Unlock()
		}
	}()

	wg.Wait()
	writeJSONWithETag(w, r, metrics)
}

type MonitoringStatus struct {
	PrometheusConfigured bool          `json:"prometheusConfigured"`
	PrometheusReachable  bool          `json:"prometheusReachable"`
	NodeExporter         *TargetStatus `json:"nodeExporter"`
	Cadvisor             *TargetStatus `json:"cadvisor"`
}

type TargetStatus struct {
	Targets int `json:"targets"`
	Nodes   int `json:"nodes"`
}

func (h *Handlers) HandleMonitoringStatus(w http.ResponseWriter, r *http.Request) {
	if h.promClient == nil {
		writeJSON(w, MonitoringStatus{})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	nodeCount := len(h.cache.ListNodes())

	var status MonitoringStatus
	status.PrometheusConfigured = true

	var mu sync.Mutex
	var wg sync.WaitGroup
	var anySuccess bool
	wg.Add(2)

	// Query node-exporter targets
	go func() {
		defer wg.Done()
		results, err := h.promClient.InstantQuery(ctx, `up{job="node-exporter"}`)
		if err != nil {
			slog.Warn("monitoring status: node-exporter query failed", "error", err)
			return
		}
		mu.Lock()
		anySuccess = true
		status.NodeExporter = &TargetStatus{Targets: len(results), Nodes: nodeCount}
		mu.Unlock()
	}()

	// Query cadvisor targets
	go func() {
		defer wg.Done()
		results, err := h.promClient.InstantQuery(ctx, `up{job="cadvisor"}`)
		if err != nil {
			slog.Warn("monitoring status: cadvisor query failed", "error", err)
			return
		}
		mu.Lock()
		anySuccess = true
		status.Cadvisor = &TargetStatus{Targets: len(results), Nodes: nodeCount}
		mu.Unlock()
	}()

	wg.Wait()

	if anySuccess {
		status.PrometheusReachable = true
	} else {
		// Fallback connectivity check
		_, err := h.promClient.InstantQuery(ctx, `vector(1)`)
		if err == nil {
			status.PrometheusReachable = true
		}
	}

	writeJSON(w, status)
}

func (h *Handlers) HandleSwarm(w http.ResponseWriter, r *http.Request) {
	if h.systemClient == nil {
		writeProblem(w, r, http.StatusNotImplemented, "swarm inspect not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	sw, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		slog.Error("swarm inspect failed", "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "swarm inspect failed")
		return
	}

	managerAddr := ""
	for _, n := range h.cache.ListNodes() {
		if n.ManagerStatus != nil && n.ManagerStatus.Leader {
			managerAddr = n.ManagerStatus.Addr
			break
		}
	}

	writeJSONWithETag(w, r, NewDetailResponse("/swarm", "Swarm", map[string]any{
		"swarm":       sw,
		"managerAddr": managerAddr,
	}))
}

func (h *Handlers) HandlePlugins(w http.ResponseWriter, r *http.Request) {
	if h.systemClient == nil {
		writeProblem(w, r, http.StatusNotImplemented, "plugin list not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	plugins, err := h.systemClient.PluginList(ctx)
	if err != nil {
		slog.Error("plugin list failed", "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "plugin list failed")
		return
	}
	if plugins == nil {
		plugins = types.PluginsListResponse{}
	}

	writeJSONWithETag(w, r, NewCollectionResponse(plugins, len(plugins), len(plugins), 0))
}

type DiskUsageSummary struct {
	Type        string `json:"type"`
	Count       int    `json:"count"`
	Active      int    `json:"active"`
	TotalSize   int64  `json:"totalSize"`
	Reclaimable int64  `json:"reclaimable"`
}

func (h *Handlers) HandleDiskUsage(w http.ResponseWriter, r *http.Request) {
	if h.systemClient == nil {
		writeProblem(w, r, http.StatusNotImplemented, "disk usage not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	du, err := h.systemClient.DiskUsage(ctx)
	if err != nil {
		slog.Error("disk usage failed", "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "disk usage failed")
		return
	}

	var summaries []DiskUsageSummary

	// Images
	var imgSize, imgReclaimable int64
	var imgActive int
	for _, img := range du.Images {
		imgSize += img.Size
		if img.Containers > 0 {
			imgActive++
		} else {
			imgReclaimable += img.Size
		}
	}
	summaries = append(summaries, DiskUsageSummary{
		Type: "images", Count: len(du.Images), Active: imgActive,
		TotalSize: imgSize, Reclaimable: imgReclaimable,
	})

	// Containers
	var ctrSize, ctrReclaimable int64
	var ctrActive int
	for _, ctr := range du.Containers {
		ctrSize += ctr.SizeRw
		if ctr.State == "running" {
			ctrActive++
		} else {
			ctrReclaimable += ctr.SizeRw
		}
	}
	summaries = append(summaries, DiskUsageSummary{
		Type: "containers", Count: len(du.Containers), Active: ctrActive,
		TotalSize: ctrSize, Reclaimable: ctrReclaimable,
	})

	// Volumes
	var volSize, volReclaimable int64
	var volActive int
	for _, vol := range du.Volumes {
		if vol.UsageData != nil {
			volSize += vol.UsageData.Size
			if vol.UsageData.RefCount > 0 {
				volActive++
			} else {
				volReclaimable += vol.UsageData.Size
			}
		}
	}
	summaries = append(summaries, DiskUsageSummary{
		Type: "volumes", Count: len(du.Volumes), Active: volActive,
		TotalSize: volSize, Reclaimable: volReclaimable,
	})

	// Build cache
	var bcSize, bcReclaimable int64
	var bcActive int
	for _, bc := range du.BuildCache {
		bcSize += bc.Size
		if bc.InUse {
			bcActive++
		} else {
			bcReclaimable += bc.Size
		}
	}
	summaries = append(summaries, DiskUsageSummary{
		Type: "buildCache", Count: len(du.BuildCache), Active: bcActive,
		TotalSize: bcSize, Reclaimable: bcReclaimable,
	})

	writeJSONWithETag(w, r, NewCollectionResponse(summaries, len(summaries), len(summaries), 0))
}

// --- Nodes ---

func (h *Handlers) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes := h.cache.ListNodes()
	nodes = searchFilter(
		nodes,
		r.URL.Query().Get("search"),
		func(n swarm.Node) string { return n.Description.Hostname },
	)
	var ok bool
	if nodes, ok = exprFilter(nodes, r.URL.Query().Get("filter"), filter.NodeEnv, w, r); !ok {
		return
	}
	p := parsePagination(r)
	nodes = sortItems(nodes, p.Sort, p.Dir, map[string]func(swarm.Node) string{
		"hostname":     func(n swarm.Node) string { return n.Description.Hostname },
		"role":         func(n swarm.Node) string { return string(n.Spec.Role) },
		"status":       func(n swarm.Node) string { return string(n.Status.State) },
		"availability": func(n swarm.Node) string { return string(n.Spec.Availability) },
	})
	resp := applyPagination(nodes, p)
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeJSONWithETag(w, r, resp)
}

func (h *Handlers) HandleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := h.cache.GetNode(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("node %q not found", id))
		return
	}
	writeJSONWithETag(w, r, NewDetailResponse("/nodes/"+id, "Node", map[string]any{
		"node": node,
	}))
}

func (h *Handlers) HandleNodeTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetNode(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("node %q not found", id))
		return
	}
	tasks := h.enrichTasks(h.cache.ListTasksByNode(id))
	writeJSONWithETag(w, r, NewCollectionResponse(tasks, len(tasks), len(tasks), 0))
}

// --- Services ---

type ServiceListItem struct {
	swarm.Service
	RunningTasks int `json:"RunningTasks"`
}

func (h *Handlers) HandleListServices(w http.ResponseWriter, r *http.Request) {
	services := h.cache.ListServices()
	services = searchFilter(
		services,
		r.URL.Query().Get("search"),
		func(s swarm.Service) string { return s.Spec.Name },
	)
	var ok bool
	if services, ok = exprFilter(
		services,
		r.URL.Query().Get("filter"),
		filter.ServiceEnv,
		w,
		r,
	); !ok {
		return
	}
	p := parsePagination(r)
	services = sortItems(services, p.Sort, p.Dir, map[string]func(swarm.Service) string{
		"name": func(s swarm.Service) string { return s.Spec.Name },
		"mode": func(s swarm.Service) string {
			if s.Spec.Mode.Global != nil {
				return "Global"
			}
			return "Replicated"
		},
	})
	paged := applyPagination(services, p)

	items := make([]ServiceListItem, len(paged.Items))
	for i, svc := range paged.Items {
		items[i] = ServiceListItem{
			Service:      svc,
			RunningTasks: h.cache.RunningTaskCount(svc.ID),
		}
	}

	writePaginationLinks(w, r, paged.Total, paged.Limit, paged.Offset)
	writeJSONWithETag(w, r, NewCollectionResponse(items, paged.Total, paged.Limit, paged.Offset))
}

func (h *Handlers) HandleGetService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("service %q not found", id))
		return
	}
	extra := map[string]any{
		"service": svc,
	}
	if changes := DiffServiceSpecs(svc.PreviousSpec, &svc.Spec); len(changes) > 0 {
		extra["changes"] = changes
	}
	writeJSONWithETag(w, r, NewDetailResponse("/services/"+id, "Service", extra))
}

func (h *Handlers) HandleServiceTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("service %q not found", id))
		return
	}
	tasks := h.enrichTasks(h.cache.ListTasksByService(id))
	writeJSONWithETag(w, r, NewCollectionResponse(tasks, len(tasks), len(tasks), 0))
}

func (h *Handlers) HandleServiceLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("service %q not found", id))
		return
	}
	h.serveLogs(
		w,
		r,
		func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error) {
			return h.dockerClient.Logs(ctx, docker.ServiceLog, id, tail, follow, since, until)
		},
	)
}

// --- Tasks ---

type EnrichedTask struct {
	swarm.Task
	ServiceName  string `json:"ServiceName,omitempty"`
	NodeHostname string `json:"NodeHostname,omitempty"`
}

func (h *Handlers) enrichTask(t swarm.Task) EnrichedTask {
	et := EnrichedTask{Task: t}
	if svc, ok := h.cache.GetService(t.ServiceID); ok {
		et.ServiceName = svc.Spec.Name
	}
	if node, ok := h.cache.GetNode(t.NodeID); ok {
		et.NodeHostname = node.Description.Hostname
	}
	return et
}

func (h *Handlers) enrichTasks(tasks []swarm.Task) []EnrichedTask {
	out := make([]EnrichedTask, len(tasks))
	for i, t := range tasks {
		out[i] = h.enrichTask(t)
	}
	return out
}

// taskStateSortKey returns a sort key that orders running tasks first,
// then starting/preparing, then terminal states alphabetically.
func taskStateSortKey(state swarm.TaskState) string {
	switch state {
	case swarm.TaskStateRunning:
		return "0"
	case swarm.TaskStateStarting:
		return "1"
	case swarm.TaskStatePreparing:
		return "1"
	case swarm.TaskStateReady:
		return "1"
	case swarm.TaskStateNew:
		return "1"
	default:
		return "2" + string(state)
	}
}

func (h *Handlers) HandleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := h.cache.ListTasks()
	var ok bool
	if tasks, ok = exprFilter(tasks, r.URL.Query().Get("filter"), filter.TaskEnv, w, r); !ok {
		return
	}
	p := parsePagination(r)
	tasks = sortItems(tasks, p.Sort, p.Dir, map[string]func(swarm.Task) string{
		"state":   func(t swarm.Task) string { return taskStateSortKey(t.Status.State) },
		"service": func(t swarm.Task) string { return t.ServiceID },
		"node":    func(t swarm.Task) string { return t.NodeID },
	})
	paged := applyPagination(tasks, p)
	writePaginationLinks(w, r, paged.Total, paged.Limit, paged.Offset)
	writeJSONWithETag(
		w,
		r,
		NewCollectionResponse(h.enrichTasks(paged.Items), paged.Total, paged.Limit, paged.Offset),
	)
}

func (h *Handlers) HandleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, ok := h.cache.GetTask(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("task %q not found", id))
		return
	}
	et := h.enrichTask(task)
	writeJSONWithETag(w, r, NewDetailResponse("/tasks/"+id, "Task", map[string]any{
		"task":    et,
		"service": map[string]any{"@id": "/services/" + et.ServiceID, "name": et.ServiceName},
		"node":    map[string]any{"@id": "/nodes/" + et.NodeID, "hostname": et.NodeHostname},
	}))
}

func (h *Handlers) HandleTaskLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetTask(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("task %q not found", id))
		return
	}
	h.serveLogs(
		w,
		r,
		func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error) {
			return h.dockerClient.Logs(ctx, docker.TaskLog, id, tail, follow, since, until)
		},
	)
}

// LogStream is an alias for the io.ReadCloser returned by Docker log APIs.
type LogStream = io.ReadCloser

// LogResponse is the JSON response for paginated log fetches.
type LogResponse struct {
	Lines   []LogLine `json:"lines"`
	Oldest  string    `json:"oldest"`
	Newest  string    `json:"newest"`
	HasMore bool      `json:"hasMore"`
}

type logFetcher func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error)

func validLogTimestamp(s string) bool {
	if s == "" {
		return true
	}
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return true
	}
	if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return true
	}
	if _, err := time.ParseDuration(s); err == nil {
		return true
	}
	return false
}

func (h *Handlers) serveLogs(w http.ResponseWriter, r *http.Request, fetch logFetcher) {
	q := r.URL.Query()
	since := q.Get("after")
	until := q.Get("before")
	streamFilter := q.Get("stream") // "", "stdout", or "stderr"
	if streamFilter != "" && streamFilter != "stdout" && streamFilter != "stderr" {
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			`invalid "stream" parameter: must be "stdout" or "stderr"`,
		)
		return
	}

	if !validLogTimestamp(since) {
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			`invalid "after" parameter: must be RFC3339 timestamp or Go duration`,
		)
		return
	}
	if !validLogTimestamp(until) {
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			`invalid "before" parameter: must be RFC3339 timestamp or Go duration`,
		)
		return
	}

	if ContentTypeFromContext(r.Context()) == ContentTypeSSE {
		if until != "" {
			writeProblem(
				w,
				r,
				http.StatusBadRequest,
				`"before" parameter is not supported for SSE log streams`,
			)
			return
		}
		h.serveLogsSSE(w, r, fetch, since, streamFilter)
		return
	}

	limit := defaultLogLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}

	// Docker ignores since/until for service logs, so we request more lines
	// than needed and filter in Go. When paginating (since or until is set),
	// use a larger tail to ensure we fetch enough lines beyond the cursor.
	tail := limit
	if since != "" || until != "" {
		tail = min(limit*10, maxLogLimit)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	logs, err := fetch(ctx, strconv.Itoa(tail), false, since, until)
	if err != nil {
		slog.Error("failed to get logs", "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to get logs")
		return
	}
	defer logs.Close() //nolint:errcheck

	// Docker's ServiceLogs with Follow=false may not close the stream
	// after sending all data. Use an idle cancel: once we've received
	// some data, if no new data arrives within 2s, cancel the context
	// so the blocked read unblocks immediately.
	lines, err := ParseDockerLogsWithIdleCancel(logs, cancel, 2*time.Second)
	if err != nil {
		slog.Error("failed to parse logs", "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to parse logs")
		return
	}
	if lines == nil {
		lines = []LogLine{}
	}

	// Docker interleaves lines from multiple tasks; sort by timestamp so
	// truncation keeps the truly newest lines.
	slices.SortStableFunc(lines, func(a, b LogLine) int {
		return strings.Compare(a.Timestamp, b.Timestamp)
	})

	// Docker ignores since/until for service logs, so enforce them here.
	if since != "" || until != "" {
		filtered := lines[:0]
		for _, l := range lines {
			if since != "" && l.Timestamp <= since {
				continue
			}
			if until != "" && l.Timestamp >= until {
				continue
			}
			filtered = append(filtered, l)
		}
		lines = filtered
	}

	// Apply stream filter before truncation so hasMore is accurate.
	if streamFilter != "" {
		filtered := lines[:0]
		for _, l := range lines {
			if l.Stream == streamFilter {
				filtered = append(filtered, l)
			}
		}
		lines = filtered
	}

	// Docker's tail=N applies per task for service logs, so the total may
	// exceed the requested limit. Truncate to the last `limit` lines.
	hasMore := len(lines) > limit
	if hasMore {
		lines = lines[len(lines)-limit:]
	}

	resp := LogResponse{Lines: lines, HasMore: hasMore}
	if len(lines) > 0 {
		resp.Oldest = lines[0].Timestamp
		resp.Newest = lines[len(lines)-1].Timestamp
	}
	writeJSON(w, resp)
}

func (h *Handlers) serveLogsSSE(
	w http.ResponseWriter,
	r *http.Request,
	fetch logFetcher,
	since, streamFilter string,
) {
	for {
		cur := activeLogSSEConns.Load()
		if cur >= maxLogSSEConns {
			w.Header().Set("Retry-After", "5")
			writeProblem(w, r, http.StatusTooManyRequests, "too many active log streams")
			return
		}
		if activeLogSSEConns.CompareAndSwap(cur, cur+1) {
			break
		}
	}
	defer activeLogSSEConns.Add(-1)

	// EventSource sends Last-Event-ID on reconnect; use it as fallback for since
	if since == "" {
		if v := r.Header.Get("Last-Event-ID"); validLogTimestamp(v) {
			since = v
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "streaming not supported")
		return
	}

	logs, err := fetch(r.Context(), "0", true, since, "")
	if err != nil {
		slog.Error("failed to stream logs", "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to stream logs")
		return
	}
	defer logs.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	ch := make(chan LogLine, 64)
	done := make(chan error, 1)
	go func() {
		done <- StreamDockerLogs(logs, ch)
		close(ch)
	}()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case line, ok := <-ch:
			if !ok {
				<-done
				return
			}
			if streamFilter != "" && line.Stream != streamFilter {
				continue
			}
			data, _ := json.Marshal(line)
			if line.Timestamp != "" {
				fmt.Fprintf(w, "id: %s\n", line.Timestamp)
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			logs.Close() // unblocks StreamDockerLogs's io.Read
			for range ch {
			}
			<-done
			return
		}
	}
}

// --- History ---

func (h *Handlers) HandleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	entries := h.cache.History().List(cache.HistoryQuery{
		Type:       q.Get("type"),
		ResourceID: q.Get("resourceId"),
		Limit:      limit,
	})
	if entries == nil {
		entries = []cache.HistoryEntry{}
	}
	writeJSONWithETag(w, r, NewCollectionResponse(entries, len(entries), len(entries), 0))
}

// --- Stacks ---

func (h *Handlers) HandleListStacks(w http.ResponseWriter, r *http.Request) {
	stacks := h.cache.ListStacks()
	stacks = searchFilter(
		stacks,
		r.URL.Query().Get("search"),
		func(s cache.Stack) string { return s.Name },
	)
	var ok bool
	if stacks, ok = exprFilter(stacks, r.URL.Query().Get("filter"), filter.StackEnv, w, r); !ok {
		return
	}
	p := parsePagination(r)
	stacks = sortItems(stacks, p.Sort, p.Dir, map[string]func(cache.Stack) string{
		"name": func(s cache.Stack) string { return s.Name },
	})
	resp := applyPagination(stacks, p)
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeJSONWithETag(w, r, resp)
}

func (h *Handlers) HandleGetStack(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	detail, ok := h.cache.GetStackDetail(name)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("stack %q not found", name))
		return
	}
	writeJSONWithETag(w, r, NewDetailResponse("/stacks/"+name, "Stack", map[string]any{
		"stack": detail,
	}))
}

const stackNamespaceLabel = "container_label_com_docker_stack_namespace"

func (h *Handlers) HandleStackSummary(w http.ResponseWriter, r *http.Request) {
	summaries := h.cache.ListStackSummaries()

	if h.promClient != nil && len(summaries) > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		var memByStack, cpuByStack map[string]float64
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			memByStack = h.queryStackMetric(ctx,
				`sum by (`+stackNamespaceLabel+`)(container_memory_usage_bytes)`)
		}()
		go func() {
			defer wg.Done()
			cpuByStack = h.queryStackMetric(
				ctx,
				`sum by (`+stackNamespaceLabel+`)(rate(container_cpu_usage_seconds_total[5m])) * 100`,
			)
		}()
		wg.Wait()

		for i := range summaries {
			summaries[i].MemoryUsageBytes = int64(memByStack[summaries[i].Name])
			summaries[i].CPUUsagePercent = cpuByStack[summaries[i].Name]
		}
	}

	if summaries == nil {
		summaries = []cache.StackSummary{}
	}
	writeJSONWithETag(w, r, NewCollectionResponse(summaries, len(summaries), len(summaries), 0))
}

func (h *Handlers) queryStackMetric(ctx context.Context, query string) map[string]float64 {
	results, err := h.promClient.InstantQuery(ctx, query)
	if err != nil {
		slog.Warn("prometheus stack metric query failed", "error", err)
		return nil
	}
	out := make(map[string]float64, len(results))
	for _, r := range results {
		if name := r.Labels[stackNamespaceLabel]; name != "" {
			out[name] = r.Value
		}
	}
	return out
}

// --- Configs ---

func (h *Handlers) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("config %q not found", id))
		return
	}
	writeJSONWithETag(w, r, NewDetailResponse("/configs/"+id, "Config", map[string]any{
		"config":   cfg,
		"services": h.cache.ServicesUsingConfig(id),
	}))
}

func (h *Handlers) HandleListConfigs(w http.ResponseWriter, r *http.Request) {
	configs := h.cache.ListConfigs()
	configs = searchFilter(
		configs,
		r.URL.Query().Get("search"),
		func(c swarm.Config) string { return c.Spec.Name },
	)
	var ok bool
	if configs, ok = exprFilter(configs, r.URL.Query().Get("filter"), filter.ConfigEnv, w, r); !ok {
		return
	}
	p := parsePagination(r)
	configs = sortItems(configs, p.Sort, p.Dir, map[string]func(swarm.Config) string{
		"name":    func(c swarm.Config) string { return c.Spec.Name },
		"created": func(c swarm.Config) string { return c.CreatedAt.String() },
		"updated": func(c swarm.Config) string { return c.UpdatedAt.String() },
	})
	resp := applyPagination(configs, p)
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeJSONWithETag(w, r, resp)
}

// --- Secrets ---

func (h *Handlers) HandleGetSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sec, ok := h.cache.GetSecret(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("secret %q not found", id))
		return
	}
	// Never expose secret data — clear it before responding.
	sec.Spec.Data = nil
	writeJSONWithETag(w, r, NewDetailResponse("/secrets/"+id, "Secret", map[string]any{
		"secret":   sec,
		"services": h.cache.ServicesUsingSecret(id),
	}))
}

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	secrets := h.cache.ListSecrets()
	for i := range secrets {
		secrets[i].Spec.Data = nil
	}
	secrets = searchFilter(
		secrets,
		r.URL.Query().Get("search"),
		func(s swarm.Secret) string { return s.Spec.Name },
	)
	var ok bool
	if secrets, ok = exprFilter(secrets, r.URL.Query().Get("filter"), filter.SecretEnv, w, r); !ok {
		return
	}
	p := parsePagination(r)
	secrets = sortItems(secrets, p.Sort, p.Dir, map[string]func(swarm.Secret) string{
		"name":    func(s swarm.Secret) string { return s.Spec.Name },
		"created": func(s swarm.Secret) string { return s.CreatedAt.String() },
		"updated": func(s swarm.Secret) string { return s.UpdatedAt.String() },
	})
	resp := applyPagination(secrets, p)
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeJSONWithETag(w, r, resp)
}

// --- Networks ---

func (h *Handlers) HandleGetNetwork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	net, ok := h.cache.GetNetwork(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("network %q not found", id))
		return
	}
	writeJSONWithETag(w, r, NewDetailResponse("/networks/"+id, "Network", map[string]any{
		"network":  net,
		"services": h.cache.ServicesUsingNetwork(id),
	}))
}

func (h *Handlers) HandleListNetworks(w http.ResponseWriter, r *http.Request) {
	networks := h.cache.ListNetworks()
	networks = searchFilter(
		networks,
		r.URL.Query().Get("search"),
		func(n network.Summary) string { return n.Name },
	)
	var ok bool
	if networks, ok = exprFilter(
		networks,
		r.URL.Query().Get("filter"),
		filter.NetworkEnv,
		w,
		r,
	); !ok {
		return
	}
	p := parsePagination(r)
	networks = sortItems(networks, p.Sort, p.Dir, map[string]func(network.Summary) string{
		"name":   func(n network.Summary) string { return n.Name },
		"driver": func(n network.Summary) string { return n.Driver },
		"scope":  func(n network.Summary) string { return n.Scope },
	})
	resp := applyPagination(networks, p)
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeJSONWithETag(w, r, resp)
}

// --- Volumes ---

func (h *Handlers) HandleGetVolume(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	vol, ok := h.cache.GetVolume(name)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("volume %q not found", name))
		return
	}
	writeJSONWithETag(w, r, NewDetailResponse("/volumes/"+name, "Volume", map[string]any{
		"volume":   vol,
		"services": h.cache.ServicesUsingVolume(name),
	}))
}

func (h *Handlers) HandleListVolumes(w http.ResponseWriter, r *http.Request) {
	volumes := h.cache.ListVolumes()
	volumes = searchFilter(
		volumes,
		r.URL.Query().Get("search"),
		func(v volume.Volume) string { return v.Name },
	)
	var ok bool
	if volumes, ok = exprFilter(volumes, r.URL.Query().Get("filter"), filter.VolumeEnv, w, r); !ok {
		return
	}
	p := parsePagination(r)
	volumes = sortItems(volumes, p.Sort, p.Dir, map[string]func(volume.Volume) string{
		"name":   func(v volume.Volume) string { return v.Name },
		"driver": func(v volume.Volume) string { return v.Driver },
		"scope":  func(v volume.Volume) string { return v.Scope },
	})
	resp := applyPagination(volumes, p)
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeJSONWithETag(w, r, resp)
}

// --- Search ---

type searchResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
	State  string `json:"state,omitempty"`
}

func labelsMatch(labels map[string]string, q string) bool {
	for k, v := range labels {
		if containsFold(k, q) || containsFold(v, q) {
			return true
		}
	}
	return false
}

func (h *Handlers) HandleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeProblem(w, r, http.StatusBadRequest, "missing required query parameter: q")
		return
	}
	if len(q) > 200 {
		writeProblem(w, r, http.StatusBadRequest, "query too long (max 200 characters)")
		return
	}
	ql := strings.ToLower(q)

	limit := 3
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			limit = n
		}
	}
	// Cap per-type results to prevent unbounded allocations on large clusters.
	const maxPerType = 1000
	if limit == 0 || limit > maxPerType {
		limit = maxPerType
	}

	type typeResults struct {
		key     string
		results []searchResult
		count   int
	}

	// Fixed-size array indexed by search type for lock-free parallel writes.
	const (
		stServices = iota
		stStacks
		stNodes
		stTasks
		stConfigs
		stSecrets
		stNetworks
		stVolumes
		stCount
	)
	var allResults [stCount]typeResults

	// Build service name lookup for tasks (needed by services + tasks searches).
	services := h.cache.ListServices()
	svcNames := make(map[string]string, len(services))
	for _, s := range services {
		svcNames[s.ID] = s.Spec.Name
	}

	var wg sync.WaitGroup
	wg.Add(stCount)

	// Services
	go func() {
		defer wg.Done()
		var matches []searchResult
		count := 0
		for _, s := range services {
			hit := containsFold(s.Spec.Name, ql)
			if !hit && s.Spec.TaskTemplate.ContainerSpec != nil {
				hit = containsFold(s.Spec.TaskTemplate.ContainerSpec.Image, ql)
			}
			if !hit {
				hit = labelsMatch(s.Spec.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					detail := ""
					if s.Spec.TaskTemplate.ContainerSpec != nil {
						detail = s.Spec.TaskTemplate.ContainerSpec.Image
						if i := strings.Index(detail, "@sha256:"); i > 0 {
							detail = detail[:i]
						}
					}
					running := h.cache.RunningTaskCount(s.ID)
					desired := 0
					if s.Spec.Mode.Replicated != nil && s.Spec.Mode.Replicated.Replicas != nil {
						desired = int(*s.Spec.Mode.Replicated.Replicas)
					} else if s.Spec.Mode.Global != nil {
						desired = -1 // global: just check running > 0
					}
					state := "running"
					if s.UpdateStatus != nil && s.UpdateStatus.State == swarm.UpdateStateUpdating {
						state = "updating"
					} else if desired == -1 {
						if running == 0 {
							state = "pending"
						}
					} else if desired > 0 && running == 0 {
						state = "failed"
					} else if running < desired {
						state = "pending"
					}
					matches = append(
						matches,
						searchResult{ID: s.ID, Name: s.Spec.Name, Detail: detail, State: state},
					)
				}
			}
		}
		allResults[stServices] = typeResults{"services", matches, count}
	}()

	// Stacks
	go func() {
		defer wg.Done()
		stacks := h.cache.ListStacks()
		var matches []searchResult
		count := 0
		for _, s := range stacks {
			if containsFold(s.Name, ql) {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     s.Name,
						Name:   s.Name,
						Detail: fmt.Sprintf("%d services", len(s.Services)),
					})
				}
			}
		}
		allResults[stStacks] = typeResults{"stacks", matches, count}
	}()

	// Nodes
	go func() {
		defer wg.Done()
		nodes := h.cache.ListNodes()
		var matches []searchResult
		count := 0
		for _, n := range nodes {
			hit := containsFold(n.Description.Hostname, ql)
			if !hit {
				hit = containsFold(n.Status.Addr, ql)
			}
			if !hit {
				hit = labelsMatch(n.Spec.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     n.ID,
						Name:   n.Description.Hostname,
						Detail: fmt.Sprintf("%s, %s", n.Spec.Role, n.Status.State),
					})
				}
			}
		}
		allResults[stNodes] = typeResults{"nodes", matches, count}
	}()

	// Tasks
	go func() {
		defer wg.Done()
		tasks := h.cache.ListTasks()
		var matches []searchResult
		count := 0
		for _, t := range tasks {
			svcName := svcNames[t.ServiceID]
			taskName := fmt.Sprintf("%s.%d", svcName, t.Slot)

			hit := containsFold(svcName, ql)
			if !hit && t.Spec.ContainerSpec != nil {
				hit = containsFold(t.Spec.ContainerSpec.Image, ql)
			}
			if !hit && t.Spec.ContainerSpec != nil {
				hit = labelsMatch(t.Spec.ContainerSpec.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					detail := ""
					if t.Spec.ContainerSpec != nil {
						detail = t.Spec.ContainerSpec.Image
						if i := strings.Index(detail, "@sha256:"); i > 0 {
							detail = detail[:i]
						}
					}
					matches = append(matches, searchResult{
						ID:     t.ID,
						Name:   taskName,
						Detail: detail,
						State:  string(t.Status.State),
					})
				}
			}
		}
		allResults[stTasks] = typeResults{"tasks", matches, count}
	}()

	// Configs
	go func() {
		defer wg.Done()
		configs := h.cache.ListConfigs()
		var matches []searchResult
		count := 0
		for _, c := range configs {
			hit := containsFold(c.Spec.Name, ql)
			if !hit {
				hit = labelsMatch(c.Spec.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     c.ID,
						Name:   c.Spec.Name,
						Detail: c.CreatedAt.Format(time.RFC3339),
					})
				}
			}
		}
		allResults[stConfigs] = typeResults{"configs", matches, count}
	}()

	// Secrets
	go func() {
		defer wg.Done()
		secrets := h.cache.ListSecrets()
		var matches []searchResult
		count := 0
		for _, s := range secrets {
			s.Spec.Data = nil
			hit := containsFold(s.Spec.Name, ql)
			if !hit {
				hit = labelsMatch(s.Spec.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     s.ID,
						Name:   s.Spec.Name,
						Detail: s.CreatedAt.Format(time.RFC3339),
					})
				}
			}
		}
		allResults[stSecrets] = typeResults{"secrets", matches, count}
	}()

	// Networks
	go func() {
		defer wg.Done()
		networks := h.cache.ListNetworks()
		var matches []searchResult
		count := 0
		for _, n := range networks {
			hit := containsFold(n.Name, ql)
			if !hit {
				hit = labelsMatch(n.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     n.ID,
						Name:   n.Name,
						Detail: n.Driver,
					})
				}
			}
		}
		allResults[stNetworks] = typeResults{"networks", matches, count}
	}()

	// Volumes
	go func() {
		defer wg.Done()
		volumes := h.cache.ListVolumes()
		var matches []searchResult
		count := 0
		for _, v := range volumes {
			hit := containsFold(v.Name, ql)
			if !hit {
				hit = labelsMatch(v.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     v.Name,
						Name:   v.Name,
						Detail: v.Driver,
					})
				}
			}
		}
		allResults[stVolumes] = typeResults{"volumes", matches, count}
	}()

	wg.Wait()

	results := make(map[string][]searchResult, stCount)
	counts := make(map[string]int, stCount)
	total := 0
	for _, s := range allResults {
		if s.count > 0 {
			results[s.key] = s.results
			counts[s.key] = s.count
			total += s.count
		}
	}

	writeJSONWithETag(w, r, NewDetailResponse("/search", "SearchResult", map[string]any{
		"query":   q,
		"results": results,
		"counts":  counts,
		"total":   total,
	}))
}
