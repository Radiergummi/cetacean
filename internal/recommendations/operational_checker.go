package recommendations

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

// OperationalChecker implements Checker for Prometheus-based operational health recommendations.
type OperationalChecker struct {
	query    QueryFunc
	cache    *cache.Cache
	lookback time.Duration
}

// NewOperationalChecker creates a new operational checker.
func NewOperationalChecker(query QueryFunc, c *cache.Cache, lookback time.Duration) *OperationalChecker {
	return &OperationalChecker{query: query, cache: c, lookback: lookback}
}

func (oc *OperationalChecker) Name() string            { return "operational" }
func (oc *OperationalChecker) Interval() time.Duration { return 5 * time.Minute }

// Check runs flaky service, disk full, and memory pressure queries in parallel.
func (oc *OperationalChecker) Check(ctx context.Context) []Recommendation {
	tickCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	type queryResult struct {
		results []queryEntry
		err     error
	}

	flakyCh := make(chan queryResult, 1)
	diskCh := make(chan queryResult, 1)
	memCh := make(chan queryResult, 1)

	lookbackStr := formatPromDuration(oc.lookback)

	go func() {
		query := fmt.Sprintf(
			`sum by (%s)(changes(container_last_seen{%s}[%s]))`,
			serviceLabelKey, serviceFilter, lookbackStr,
		)
		entries, err := queryEntries(tickCtx, oc.query, query, serviceLabelKey)
		flakyCh <- queryResult{entries, err}
	}()

	go func() {
		query := `max by (instance)(` +
			`(1 - node_filesystem_avail_bytes{mountpoint="/",fstype!="tmpfs"} / ` +
			`node_filesystem_size_bytes{mountpoint="/",fstype!="tmpfs"}) * 100)`
		entries, err := queryEntries(tickCtx, oc.query, query, "instance")
		diskCh <- queryResult{entries, err}
	}()

	go func() {
		query := `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100`
		entries, err := queryEntries(tickCtx, oc.query, query, "instance")
		memCh <- queryResult{entries, err}
	}()

	flakyResult := <-flakyCh
	diskResult := <-diskCh
	memResult := <-memCh

	if flakyResult.err != nil {
		slog.Warn("operational: flaky service query failed", "error", flakyResult.err)
	}

	if diskResult.err != nil {
		slog.Warn("operational: disk usage query failed", "error", diskResult.err)
	}

	if memResult.err != nil {
		slog.Warn("operational: memory pressure query failed", "error", memResult.err)
	}

	var recs []Recommendation

	if flakyResult.err == nil {
		recs = append(recs, oc.flakyServiceRecs(flakyResult.results)...)
	}

	if diskResult.err == nil {
		recs = append(recs, oc.nodeRecs(diskResult.results, CategoryNodeDiskFull, "disk usage")...)
	}

	if memResult.err == nil {
		recs = append(recs, oc.nodeRecs(memResult.results, CategoryNodeMemPressure, "memory usage")...)
	}

	return recs
}

func (oc *OperationalChecker) flakyServiceRecs(entries []queryEntry) []Recommendation {
	serviceNameToID := make(map[string]string)
	for _, svc := range oc.cache.ListServices() {
		serviceNameToID[svc.Spec.Name] = svc.ID
	}

	var recs []Recommendation

	for _, entry := range entries {
		restarts := entry.value
		if restarts <= 5 {
			continue
		}

		targetID := serviceNameToID[entry.key]
		recs = append(recs, Recommendation{
			Category:   CategoryFlakyService,
			Severity:   SeverityWarning,
			Scope:      ScopeService,
			TargetID:   targetID,
			TargetName: entry.key,
			Message:    fmt.Sprintf("Service has had %d task restarts over the past %s", int(math.Round(restarts)), formatPromDuration(oc.lookback)),
		})
	}

	return recs
}

func (oc *OperationalChecker) nodeRecs(entries []queryEntry, category Category, resource string) []Recommendation {
	nodesByAddr := make(map[string]nodeRef)
	nodesByHostname := make(map[string]nodeRef)

	for _, node := range oc.cache.ListNodes() {
		ref := nodeRef{id: node.ID, hostname: node.Description.Hostname}
		if node.Status.Addr != "" {
			nodesByAddr[node.Status.Addr] = ref
		}

		if node.Description.Hostname != "" {
			nodesByHostname[node.Description.Hostname] = ref
		}
	}

	var recs []Recommendation

	for _, entry := range entries {
		usage := entry.value
		if usage <= 90 {
			continue
		}

		targetName := entry.key
		targetID := ""

		if ref, ok := nodesByAddr[entry.key]; ok {
			targetName = ref.hostname
			targetID = ref.id
		} else if ref, ok := nodesByHostname[entry.key]; ok {
			targetName = ref.hostname
			targetID = ref.id
		}

		recs = append(recs, Recommendation{
			Category:   category,
			Severity:   SeverityCritical,
			Scope:      ScopeNode,
			TargetID:   targetID,
			TargetName: targetName,
			Message:    fmt.Sprintf("Node %s is at %.0f%%", resource, usage),
		})
	}

	return recs
}

type nodeRef struct {
	id       string
	hostname string
}

type queryEntry struct {
	key   string
	value float64
}

func queryEntries(ctx context.Context, query QueryFunc, promQuery, labelKey string) ([]queryEntry, error) {
	results, err := query(ctx, promQuery)
	if err != nil {
		return nil, err
	}

	entries := make([]queryEntry, 0, len(results))
	for _, r := range results {
		if key := r.Labels[labelKey]; key != "" {
			entries = append(entries, queryEntry{key: key, value: r.Value})
		}
	}

	return entries, nil
}
