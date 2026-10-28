package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
)

func (h *Handlers) getLocalNodeID() string {
	h.localNodeMu.Lock()
	defer h.localNodeMu.Unlock()
	if h.localNodeID != "" {
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
			h.localNodeRetryAfter = nil
		}
	}
	return h.localNodeID
}

func (h *Handlers) HandleCluster(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}
	snap := h.cache.Snapshot()
	// Counted from one list, so the two counts never exceed the total.
	services := h.cache.ListServices()
	converged, degraded := cluster.CountServiceStates(
		snap,
		services,
		h.cache.EligibleNodeCounts(services...),
	)
	overview := ClusterOverviewResponse{
		NodeCount:            snap.NodeCount,
		ServiceCount:         len(services),
		TaskCount:            snap.TaskCount,
		StackCount:           snap.StackCount,
		TasksByState:         snap.TasksByState,
		NodesReady:           snap.NodesReady,
		NodesDown:            snap.NodesDown,
		NodesDraining:        snap.NodesDraining,
		ServicesConverged:    converged,
		ServicesDegraded:     degraded,
		ReservedCPU:          snap.ReservedCPU,
		ReservedMemory:       snap.ReservedMemory,
		TotalCPU:             snap.TotalCPU,
		TotalMemory:          snap.TotalMemory,
		Quorum:               snap.Quorum,
		PrometheusConfigured: h.promClient != nil,
		LocalNodeID:          h.getLocalNodeID(),
	}
	writeCachedJSON(w, r, NewDetailResponse(r.Context(), "/cluster", "Cluster", overview))
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
	if !h.requireAnyGrant(w, r) {
		return
	}

	nodes := h.cache.NodeTotals()
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(
			r.Context(),
			"/cluster/capacity",
			"ClusterCapacity",
			ClusterCapacityResponse{
				MaxNodeCPU:    nodes.MaxNodeCPU,
				MaxNodeMemory: nodes.MaxNodeMemory,
				TotalCPU:      nodes.TotalCPU,
				TotalMemory:   nodes.TotalMemory,
				NodeCount:     nodes.Count,
			},
		),
	)
}

func (h *Handlers) HandleClusterMetrics(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}
	if h.promClient == nil {
		problem.Write(w, r, "MTR001", "prometheus not configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	nodes := h.cache.NodeTotals()

	// Each query writes only its own locals; the metrics are assembled after Wait.
	var (
		wg                   sync.WaitGroup
		cpuPercent, memUsed  float64
		cpuOK, memOK         bool
		diskTotal, diskAvail float64
	)

	wg.Go(func() {
		results, err := h.promClient.InstantQuery(
			ctx,
			`sum(rate(node_cpu_seconds_total{mode!="idle"}[5m])) / sum(rate(node_cpu_seconds_total[5m])) * 100`,
		)
		if err != nil {
			slog.Warn("cluster metrics: CPU query failed", "error", err)
			return
		}
		if len(results) > 0 {
			cpuPercent, cpuOK = results[0].Value, true
		}
	})

	wg.Go(func() {
		results, err := h.promClient.InstantQuery(ctx,
			`sum(node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes)`)
		if err != nil {
			slog.Warn("cluster metrics: memory query failed", "error", err)
			return
		}
		if len(results) > 0 {
			memUsed, memOK = results[0].Value, true
		}
	})

	wg.Go(func() {
		results, err := h.promClient.InstantQuery(
			ctx,
			`sum(node_filesystem_size_bytes{mountpoint="/"})`,
		)
		if err == nil && len(results) > 0 {
			diskTotal = results[0].Value
		}
	})

	wg.Go(func() {
		results, err := h.promClient.InstantQuery(
			ctx,
			`sum(node_filesystem_avail_bytes{mountpoint="/"})`,
		)
		if err == nil && len(results) > 0 {
			diskAvail = results[0].Value
		}
	})

	wg.Wait()

	var metrics ClusterMetrics
	if cpuOK {
		metrics.CPU = ResourceMetric{
			Used:    float64(nodes.TotalCPU) * cpuPercent / 100,
			Total:   float64(nodes.TotalCPU),
			Percent: cpuPercent,
		}
	}
	if memOK {
		total := float64(nodes.TotalMemory)
		pct := 0.0
		if total > 0 {
			pct = memUsed / total * 100
		}
		metrics.Memory = ResourceMetric{Used: memUsed, Total: total, Percent: pct}
	}
	if diskTotal > 0 {
		used := diskTotal - diskAvail
		metrics.Disk = ResourceMetric{
			Used:    used,
			Total:   diskTotal,
			Percent: used / diskTotal * 100,
		}
	}

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/cluster/metrics", "ClusterMetrics", metrics),
	)
}

type MonitoringStatus struct {
	PrometheusConfigured bool          `json:"prometheusConfigured"`
	PrometheusReachable  bool          `json:"prometheusReachable"`
	Queryable            bool          `json:"queryable"`
	Error                string        `json:"error,omitempty"`
	NodeExporter         *TargetStatus `json:"nodeExporter"`
	Cadvisor             *TargetStatus `json:"cadvisor"`
}

type TargetStatus struct {
	Targets int `json:"targets"`
	Nodes   int `json:"nodes"`
}

func (h *Handlers) HandleMonitoringStatus(w http.ResponseWriter, r *http.Request) {
	if !acceptable(r, ContentTypeJSON) {
		notAcceptable(w, r, "application/json")
		return
	}

	if !h.requireAnyGrant(w, r) {
		return
	}
	allowReads(w)

	queryable := h.acl.CanReadEverything(auth.IdentityFromContext(r.Context()))

	if h.promClient == nil {
		writeCachedJSON(
			w,
			r,
			NewDetailResponse(
				r.Context(),
				"/metrics/status",
				"MonitoringStatus",
				MonitoringStatus{Queryable: queryable},
			),
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	nodeCount := h.cache.NodeCount()

	var status MonitoringStatus
	status.PrometheusConfigured = true
	status.Queryable = queryable

	var (
		wg                     sync.WaitGroup
		nodeExporter, cadvisor *TargetStatus
	)

	// Detected by node-exporter's own metrics, never by a `job` label: the job
	// name belongs to whoever wrote the Prometheus config. node_uname_info is
	// emitted in every deployment, and is what maps a Swarm node to an instance
	// label -- the question the UI actually depends on.
	wg.Go(func() {
		results, err := h.promClient.InstantQuery(ctx, `count by (instance) (node_uname_info)`)
		if err != nil {
			slog.Warn("monitoring status: node-exporter query failed", "error", err)
			return
		}
		nodeExporter = &TargetStatus{Targets: len(results), Nodes: nodeCount}
	})

	// Query cadvisor targets
	wg.Go(func() {
		results, err := h.promClient.InstantQuery(ctx, `up{job="cadvisor"}`)
		if err != nil {
			slog.Warn("monitoring status: cadvisor query failed", "error", err)
			return
		}
		cadvisor = &TargetStatus{Targets: len(results), Nodes: nodeCount}
	})

	wg.Wait()
	status.NodeExporter, status.Cadvisor = nodeExporter, cadvisor

	if nodeExporter != nil || cadvisor != nil {
		status.PrometheusReachable = true
	} else {
		// Fallback connectivity check
		_, err := h.promClient.InstantQuery(ctx, `vector(1)`)
		if err == nil {
			status.PrometheusReachable = true
		} else {
			status.Error = err.Error()
		}
	}

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/metrics/status", "MonitoringStatus", status),
	)
}

func (h *Handlers) HandleSwarm(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	if h.systemClient == nil {
		problem.Write(w, r, "SWM001", "swarm inspect not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	sw, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		slog.Error("swarm inspect failed", "error", err)
		problem.Write(w, r, "SWM002", "swarm inspect failed")
		return
	}

	managerAddr := ""
	h.cache.EachNode(func(n swarm.Node) bool {
		if n.ManagerStatus != nil && n.ManagerStatus.Leader {
			managerAddr = n.ManagerStatus.Addr
			return false
		}

		return true
	})

	h.setAllow(w, r, "swarm", "cluster")

	// A join token admits a node to the cluster, so it is held to the tier
	// that rotates it, not just to the swarm write grant.
	if h.levelFor(r) < config.OpsImpactful ||
		!h.acl.Can(auth.IdentityFromContext(r.Context()), "write", "swarm:cluster") {
		sw.JoinTokens = swarm.JoinTokens{}
	}

	// No Last-Modified: the leader's address and the redaction change without
	// the swarm's timestamp moving.
	writeCachedJSON(w, r, NewDetailResponse(r.Context(), "/swarm", "Swarm", SwarmResponse{
		Swarm:       sw,
		ManagerAddr: managerAddr,
	}))
}

type DiskUsageSummary struct {
	Type        string `json:"type"`
	Count       int    `json:"count"`
	Active      int    `json:"active"`
	TotalSize   int64  `json:"totalSize"`
	Reclaimable int64  `json:"reclaimable"`
}

const (
	// diskUsageTTL is how long a disk usage result is reused. The daemon walks
	// every image, container and volume to answer, and any grant may ask.
	diskUsageTTL      = 30 * time.Second
	diskUsageErrorTTL = 5 * time.Second
	diskUsageTimeout  = 10 * time.Second
)

// diskUsageCache holds the last result, failures included; the lock is held
// across a fetch, so a burst of callers waits for one daemon call.
type diskUsageCache struct {
	mu        sync.Mutex
	value     types.DiskUsage
	err       error
	fetchedAt time.Time
}

func (c *diskUsageCache) get(
	ctx context.Context,
	fetch func(context.Context) (types.DiskUsage, error),
) (types.DiskUsage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ttl := diskUsageTTL
	if c.err != nil {
		ttl = diskUsageErrorTTL
	}

	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < ttl {
		return c.value, c.err
	}

	// Detached: callers queued on the lock share the result, so one leaving must not fail it.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), diskUsageTimeout)
	defer cancel()

	c.value, c.err = fetch(fetchCtx)
	c.fetchedAt = time.Now()

	return c.value, c.err
}

func (h *Handlers) HandleDiskUsage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	if h.systemClient == nil {
		problem.Write(w, r, "SWM004", "disk usage not available")
		return
	}

	du, err := h.diskUsage.get(r.Context(), h.systemClient.DiskUsage)
	if err != nil {
		slog.Error("disk usage failed", "error", err)
		problem.Write(w, r, "SWM005", "disk usage failed")
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

	writeCachedJSON(
		w,
		r,
		NewCollectionResponse(
			r.Context(),
			wrapItems(
				r.Context(),
				summaries,
				"DiskUsageSummary",
				func(d DiskUsageSummary) string { return "/disk-usage#" + d.Type },
			),
			len(summaries),
			len(summaries),
			0,
		),
	)
}
