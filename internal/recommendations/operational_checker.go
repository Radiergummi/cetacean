package recommendations

import (
	"context"
	"fmt"
	"log/slog"
	"net"
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
func NewOperationalChecker(
	query QueryFunc,
	c *cache.Cache,
	lookback time.Duration,
) *OperationalChecker {
	return &OperationalChecker{query: query, cache: c, lookback: lookback}
}

func (oc *OperationalChecker) Name() string            { return "operational" }
func (oc *OperationalChecker) Interval() time.Duration { return 5 * time.Minute }

// Check derives flaky-service recommendations from the cache and, when
// Prometheus is configured, runs disk-full and memory-pressure queries.
func (oc *OperationalChecker) Check(ctx context.Context) []Recommendation {
	recs := oc.flakyServiceRecs()

	if oc.query == nil {
		return recs
	}

	tickCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	type queryResult struct {
		results []queryEntry
		err     error
	}

	diskCh := make(chan queryResult, 1)
	memCh := make(chan queryResult, 1)

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

	diskResult := <-diskCh
	memResult := <-memCh

	if diskResult.err != nil {
		slog.Warn("operational: disk usage query failed", "error", diskResult.err)
	}

	if memResult.err != nil {
		slog.Warn("operational: memory pressure query failed", "error", memResult.err)
	}

	if diskResult.err == nil {
		recs = append(recs, oc.nodeRecs(diskResult.results, CategoryNodeDiskFull, "disk usage")...)
	}

	if memResult.err == nil {
		recs = append(
			recs,
			oc.nodeRecs(memResult.results, CategoryNodeMemPressure, "memory usage")...)
	}

	return recs
}

func (oc *OperationalChecker) flakyServiceRecs() []Recommendation {
	var recs []Recommendation

	// The tracker is built at startup, so on a young process the lookback in
	// the message below is longer than the period actually counted. Saying
	// "over the past 7d" for half an hour of observation understates a chronic
	// fault by orders of magnitude and makes it read as new, so the window is
	// reported as the shorter of the two.
	counted := oc.lookback
	if since := time.Since(oc.cache.RestartTrackingSince()); since < counted {
		counted = since
	}

	for _, svc := range oc.cache.ListServices() {
		count := oc.cache.RestartCount(svc.ID, oc.lookback)
		if count <= 5 {
			continue
		}

		recs = append(recs, Recommendation{
			Category:   CategoryFlakyService,
			Severity:   SeverityWarning,
			Scope:      ScopeService,
			TargetID:   svc.ID,
			TargetName: svc.Spec.Name,
			Message: fmt.Sprintf(
				"Service has had %d task failures over the past %s",
				count,
				formatPromDuration(counted),
			),
		})
	}

	return recs
}

func (oc *OperationalChecker) nodeRecs(
	entries []queryEntry,
	category Category,
	resource string,
) []Recommendation {
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

		// Strip port from the Prometheus instance label (e.g. "10.0.0.5:9100" → "10.0.0.5",
		// "myhost:9100" → "myhost") before matching against swarm node addresses/hostnames.
		host := entry.key
		if h, _, err := net.SplitHostPort(entry.key); err == nil {
			host = h
		}

		if ref, ok := nodesByAddr[host]; ok {
			targetName = ref.hostname
			targetID = ref.id
		} else if ref, ok := nodesByHostname[host]; ok {
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

func queryEntries(
	ctx context.Context,
	query QueryFunc,
	promQuery, labelKey string,
) ([]queryEntry, error) {
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
