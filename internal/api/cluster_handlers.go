package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

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
	writeJSONWithETag(w, r, NewDetailResponse(r.Context(), "/cluster", "Cluster", extra))
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
	writeJSONWithETag(w, r, NewDetailResponse(r.Context(), "/cluster/capacity", "ClusterCapacity", extra))
}

func (h *Handlers) HandleClusterMetrics(w http.ResponseWriter, r *http.Request) {
	if h.promClient == nil {
		writeErrorCode(w, r, "MTR001", "prometheus not configured")
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
		writeErrorCode(w, r, "SWM001", "swarm inspect not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	sw, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		slog.Error("swarm inspect failed", "error", err)
		writeErrorCode(w, r, "SWM002", "swarm inspect failed")
		return
	}

	managerAddr := ""
	for _, n := range h.cache.ListNodes() {
		if n.ManagerStatus != nil && n.ManagerStatus.Leader {
			managerAddr = n.ManagerStatus.Addr
			break
		}
	}

	writeJSONWithETag(w, r, NewDetailResponse(r.Context(), "/swarm", "Swarm", map[string]any{
		"swarm":       sw,
		"managerAddr": managerAddr,
	}))
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
		writeErrorCode(w, r, "SWM004", "disk usage not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	du, err := h.systemClient.DiskUsage(ctx)
	if err != nil {
		slog.Error("disk usage failed", "error", err)
		writeErrorCode(w, r, "SWM005", "disk usage failed")
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

	writeJSONWithETag(w, r, NewCollectionResponse(r.Context(), summaries, len(summaries), len(summaries), 0))
}
