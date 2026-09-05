package mcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// metricTargetCluster ranks across the cluster rather than charting one
// resource. It is the third value `target` takes, beside service and node.
const metricTargetCluster = "cluster"

// What a ranking ranks. Cluster-wide, a caller means either "which service is
// eating CPU" or "which host is hot"; they are different questions over
// different exporters, and neither is a sensible default for the other.
const (
	rankByService = "service"
	rankByNode    = "node"
)

const (
	defaultRankTop = 5

	// maxRankTop caps a ranking. A legend of forty series is not a chart
	// anyone reads, and every series is a full set of points a model holds.
	maxRankTop = 10
)

// rankSpec is one (ranked-member, metric) pair: the label that names each
// member, the unit, and the PromQL to rank by.
//
// The queries are separate from metricCatalog's rather than derived from them
// because the shape differs at the aggregation: charting one resource sums
// everything selected into a single line, while ranking groups by the label
// that distinguishes members and takes the top N. Deriving one from the other
// would mean rewriting an aggregation by string surgery.
type rankSpec struct {
	label string
	unit  string

	// query takes the top count and the scope selector, in that order. The
	// selector may be empty, so every query places it after a fixed matcher
	// and a trailing comma is never left dangling.
	query string
}

var rankCatalog = map[string]map[string]rankSpec{
	rankByService: {
		metricCPU: {
			label: swarmServiceLabel,
			unit:  "percent",
			query: `topk(%d, sum by (` + swarmServiceLabel +
				`) (rate(container_cpu_usage_seconds_total{` + swarmServiceLabel +
				`!=""%s}[5m])) * 100)`,
		},
		metricMemory: {
			label: swarmServiceLabel,
			unit:  "bytes",
			query: `topk(%d, sum by (` + swarmServiceLabel +
				`) (container_memory_usage_bytes{` + swarmServiceLabel + `!=""%s}))`,
		},
		metricNetwork: {
			label: swarmServiceLabel,
			unit:  "bytes/s",
			// Receive and transmit summed: a ranking asks which member moves
			// the most traffic, and splitting each member into two series
			// doubles the legend to answer a question nobody asked of a
			// ranking. The per-resource chart still separates them.
			query: `topk(%d, sum by (` + swarmServiceLabel +
				`) (rate(container_network_receive_bytes_total{` + swarmServiceLabel +
				`!=""%[2]s}[5m]) + rate(container_network_transmit_bytes_total{` +
				swarmServiceLabel + `!=""%[2]s}[5m])))`,
		},
	},
	rankByNode: {
		metricCPU: {
			label: "instance",
			unit:  "percent",
			query: `topk(%d, 100 - (avg by (instance) ` +
				`(rate(node_cpu_seconds_total{mode="idle"%s}[5m])) * 100))`,
		},
		metricMemory: {
			label: "instance",
			unit:  "percent",
			query: `topk(%d, (1 - node_memory_MemAvailable_bytes{__scope__} ` +
				`/ node_memory_MemTotal_bytes{__scope__}) * 100)`,
		},
		metricNetwork: {
			label: "instance",
			unit:  "bytes/s",
			query: `topk(%d, sum by (instance) ` +
				`(rate(node_network_receive_bytes_total{device!="lo"%[2]s}[5m]) + ` +
				`rate(node_network_transmit_bytes_total{device!="lo"%[2]s}[5m])))`,
		},
	},
}

// rankQuery renders a spec's PromQL for a top count and a scope selector.
//
// The node memory query is the one that cannot take a leading comma, because
// its selector is the whole label set of two separate metrics rather than an
// addition to an existing matcher — hence the placeholder rather than a
// format verb.
func rankQuery(spec rankSpec, top int, scope string) string {
	if strings.Contains(spec.query, "__scope__") {
		return fmt.Sprintf(
			strings.ReplaceAll(spec.query, "{__scope__}", "{"+strings.TrimPrefix(scope, ",")+"}"),
			top,
		)
	}

	return fmt.Sprintf(spec.query, top, scope)
}

// rankScope decides what a ranking ranks across, and returns the PromQL
// selector restricting it — always with a leading comma, or empty.
//
// A caller's grants have to reach the query rather than the result. Ranking
// the whole cluster and filtering afterwards would hand a restricted caller an
// empty answer whenever the true top N happen to all be invisible to them,
// which is both wrong and impossible for them to distinguish from an idle
// cluster.
func (s *Server) rankScope(ctx context.Context, by string) (string, error) {
	if s.acl == nil {
		return "", nil
	}

	switch by {
	case rankByService:
		services := s.filterServices(ctx, s.cache.ListServices())

		names := make([]string, 0, len(services))
		for _, svc := range services {
			names = append(names, promQLRegexValue(svc.Spec.Name))
		}

		if len(names) == 0 {
			return "", fmt.Errorf("no services you can read, so there is nothing to rank")
		}

		return fmt.Sprintf(`,%s=~"%s"`, swarmServiceLabel, strings.Join(names, "|")), nil

	default:
		nodes := s.filterNodes(ctx, s.cache.ListNodes())

		hosts := make([]string, 0, len(nodes))
		for _, node := range nodes {
			if host := instanceHost(node); host != "" {
				hosts = append(hosts, promQLRegexValue(host))
			}
		}

		if len(hosts) == 0 {
			return "", fmt.Errorf("no nodes you can read, so there is nothing to rank")
		}

		return fmt.Sprintf(`,instance=~"(%s):.*"`, strings.Join(hosts, "|")), nil
	}
}

// instanceHost is the host half of the `instance` label for a node, by the
// same rule instanceSelector follows: the address is the reliable half of the
// pair and the hostname is the fallback.
func instanceHost(node swarm.Node) string {
	if address := node.Status.Addr; address != "" {
		return address
	}

	return node.Description.Hostname
}

// nameRankedSeries turns a Prometheus label into the name the cluster uses.
//
// For services the label is Docker's own service name and already is that. For
// nodes it is `instance` — host:port, which is neither Docker's nor
// Cetacean's — so it is resolved back through the cache. An instance no node
// claims keeps its raw label rather than being dropped: dropping it would
// silently shorten a ranking, which is the one thing a ranking must not do.
func (s *Server) nameRankedSeries(by, label string) string {
	if by != rankByNode || label == "" {
		return label
	}

	host, _, found := strings.Cut(label, ":")
	if !found {
		host = label
	}

	for _, node := range s.cache.ListNodes() {
		if node.Status.Addr == host || node.Description.Hostname == host {
			return nodeACLName(node)
		}
	}

	return label
}

// rankMetrics answers the ranking form of get_metrics.
//
// Two questions reach it. `target: "cluster"` ranks the cluster's own members
// — services by default, nodes when `by` says so. `target: "node"` with an id
// ranks the services running on that one node, which is the "why is this host
// hot?" question and the only one that needs a scope resolved from the cache.
//
// The result is a metricsResult like any other, one series per ranked member,
// so nothing downstream — schema, widget, or reader — learns a second shape.
func (s *Server) rankMetrics(
	ctx context.Context,
	req mcplib.CallToolRequest,
	target, metric, rangeKey string,
	top int,
) (string, error) {
	if top <= 0 {
		top = defaultRankTop
	}
	if top > maxRankTop {
		top = maxRankTop
	}

	window, ok := metricRanges[rangeKey]
	if !ok {
		return "", fmt.Errorf("unknown range %q; expected one of %v", rangeKey, rangeNames())
	}

	by, scope, id, name, err := s.rankTarget(ctx, req, target)
	if err != nil {
		return "", err
	}

	spec, ok := rankCatalog[by][metric]
	if !ok {
		return "", fmt.Errorf(
			"unknown metric %q; expected one of %v",
			metric, metricNames(metricCatalog[metricTargetService]),
		)
	}

	if s.prom == nil {
		return "", errors.New(
			"metrics are unavailable: this Cetacean has no Prometheus configured (CETACEAN_PROMETHEUS_URL)",
		)
	}

	end := time.Now()
	start := end.Add(-window.window)

	results, err := s.prom.RangeQuery(
		ctx,
		rankQuery(spec, top, scope),
		strconv.FormatInt(start.Unix(), 10),
		strconv.FormatInt(end.Unix(), 10),
		strconv.Itoa(int(window.step.Seconds()))+"s",
	)
	if err != nil {
		return "", fmt.Errorf("query metrics: %w", err)
	}

	series := make([]metricSeries, 0, len(results))

	for _, result := range results {
		points := make([]metricPoint, 0, len(result.Points))

		for _, point := range result.Points {
			points = append(points, metricPoint{
				Time:  promTime(point.Timestamp),
				Value: point.Value,
			})
		}

		series = append(series, metricSeries{
			Name:   s.nameRankedSeries(by, result.Labels[spec.label]),
			Points: points,
		})
	}

	// An empty ranking is the same ambiguity a single empty series is, and it
	// has the same answer: say the exporter is missing rather than imply the
	// cluster is idle.
	probeTarget := metricTargetService
	if by == rankByNode {
		probeTarget = metricTargetNode
	}

	if err := s.requireExporter(ctx, probeTarget, series, start, end, window.step); err != nil {
		return "", err
	}

	return marshalResult(metricsResult{
		Target: target,
		ID:     id,
		Name:   name,
		Metric: metric,
		Unit:   spec.unit,
		Range:  rangeKey,
		Series: series,
	})
}

// rankTarget resolves what a ranking ranks and what it ranks across, reporting
// the member kind, the PromQL scope selector, and the id and name identifying
// the thing being broken down (empty for the cluster itself).
func (s *Server) rankTarget(
	ctx context.Context,
	req mcplib.CallToolRequest,
	target string,
) (by, scope, id, name string, err error) {
	switch target {
	case metricTargetCluster:
		// A cluster is not one resource, so naming one is a contradiction
		// rather than a narrowing — and silently ignoring it would answer a
		// different question than the caller asked.
		if req.GetString("id", "") != "" {
			return "", "", "", "", fmt.Errorf(
				"target %q ranks across the cluster and takes no id; name a node as the target to rank within one",
				metricTargetCluster,
			)
		}

		by = req.GetString("by", rankByService)
		if _, ok := rankCatalog[by]; !ok {
			return "", "", "", "", fmt.Errorf(
				"unknown by %q; expected %q or %q", by, rankByService, rankByNode,
			)
		}

		scope, err = s.rankScope(ctx, by)

		return by, scope, "", metricTargetCluster, err

	case metricTargetNode:
		id, err = req.RequireString("id")
		if err != nil {
			return "", "", "", "", err
		}

		// The scope is the node's own `instance`, which every exporter scraped
		// on that host carries. cAdvisor's containers are reachable no other
		// standard way: node_hostname is a relabel Cetacean's shipped
		// prometheus.yml defines, so a cluster with its own config would not
		// have it.
		name, scope, err = s.nodeMetricSelector(ctx, id)
		if err != nil {
			return "", "", "", "", err
		}

		// The instance narrows the query to that host, but this branch ranks
		// *services* — so the caller's service grants have to reach the query
		// as well, exactly as the cluster branch arranges. Without them, a
		// caller holding a node grant is told the name and load of every
		// service running on it, which is the disclosure rankScope exists to
		// prevent.
		services, err := s.rankScope(ctx, rankByService)
		if err != nil {
			return "", "", "", "", err
		}

		return rankByService, "," + scope + services, id, name, nil

	default:
		return "", "", "", "", fmt.Errorf(
			"target %q cannot be broken down: a service's contributors are its tasks, which this tool does not chart",
			target,
		)
	}
}
