package mcp

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/prom"
)

// MetricsQuerier is the narrow surface of the Prometheus client the MCP server
// needs. *prometheus.Client satisfies it; nil means Prometheus is not
// configured, and get_metrics then says so rather than returning empty charts.
type MetricsQuerier interface {
	RangeQuery(ctx context.Context, query, start, end, step string) ([]prom.Series, error)
}

// Targets and metrics get_metrics accepts. Both are closed sets: an agent picks
// a target and a metric, and Cetacean owns the PromQL. A tool taking a raw
// query would be more flexible and would also hand the caller a way around
// every ACL grant, since the label selector would be theirs rather than ours.
const (
	metricTargetService = "service"
	metricTargetNode    = "node"

	metricCPU     = "cpu"
	metricMemory  = "memory"
	metricNetwork = "network"
)

// swarmServiceLabel is the label cAdvisor derives from Docker's own
// com.docker.swarm.service.name, and is how a container is attributed to a
// service. The dashboard queries by the same label.
const swarmServiceLabel = "container_label_com_docker_swarm_service_name"

// metricRange is a window and the resolution to sample it at. The steps keep
// every window between 60 and 170 points: enough to see the shape, few enough
// that a widget frame renders it without thinning.
type metricRange struct {
	window time.Duration
	step   time.Duration
}

var metricRanges = map[string]metricRange{
	"1h":  {window: time.Hour, step: time.Minute},
	"6h":  {window: 6 * time.Hour, step: 5 * time.Minute},
	"24h": {window: 24 * time.Hour, step: 15 * time.Minute},
	"7d":  {window: 7 * 24 * time.Hour, step: time.Hour},
}

const defaultMetricRange = "1h"

// metricQuery is one series of a metric: the name it is reported under and the
// PromQL that produces it, with `%s` where the resolved selector goes.
type metricQuery struct {
	series string
	query  string
}

// metricSpec is what one (target, metric) pair produces — a unit and one or
// more series. Network is two series because receive and transmit are the
// question; the others are one.
type metricSpec struct {
	unit    string
	queries []metricQuery
}

// exporterProbe answers "is anything collecting for this target at all?",
// asked only when every series came back empty: an idle service and a cluster
// with no cAdvisor look alike. The test is the exporter's own metric family,
// never a `job` label, plus the Swarm service label for a service probe.
type exporterProbe struct {
	query   string
	missing string
}

var exporterProbes = map[string]exporterProbe{
	metricTargetService: {
		query:   `count(container_cpu_usage_seconds_total{` + swarmServiceLabel + `!=""})`,
		missing: "cAdvisor is not reporting per-container metrics for this cluster",
	},
	metricTargetNode: {
		query:   `count(node_uname_info)`,
		missing: "node-exporter is not reporting node metrics for this cluster",
	},
}

// metricCatalog holds every query get_metrics can run. These mirror the ones
// the dashboard composes in the browser — a second copy, since there is no
// server-side query layer to share. Keep the two in step: a metric that reads
// differently here than on the dashboard is worse than one that is missing.
var metricCatalog = map[string]map[string]metricSpec{
	metricTargetService: {
		metricCPU: {
			unit: "percent",
			queries: []metricQuery{{
				series: "cpu",
				query:  `sum(rate(container_cpu_usage_seconds_total{%s}[5m])) * 100`,
			}},
		},
		metricMemory: {
			unit: "bytes",
			queries: []metricQuery{{
				series: "memory",
				query:  `sum(container_memory_usage_bytes{%s})`,
			}},
		},
		metricNetwork: {
			unit: "bytes/s",
			queries: []metricQuery{
				{
					series: "receive",
					query:  `sum(rate(container_network_receive_bytes_total{%s}[5m]))`,
				},
				{
					series: "transmit",
					query:  `sum(rate(container_network_transmit_bytes_total{%s}[5m]))`,
				},
			},
		},
	},
	metricTargetNode: {
		metricCPU: {
			unit: "percent",
			queries: []metricQuery{{
				series: "cpu",
				query:  `100 - (avg(rate(node_cpu_seconds_total{mode="idle",%s}[5m])) * 100)`,
			}},
		},
		metricMemory: {
			unit: "percent",
			queries: []metricQuery{
				{
					series: "memory",
					query:  `(1 - node_memory_MemAvailable_bytes{%[1]s} / node_memory_MemTotal_bytes{%[1]s}) * 100`,
				},
			},
		},
		metricNetwork: {
			unit: "bytes/s",
			queries: []metricQuery{
				{
					series: "receive",
					query:  `sum(rate(node_network_receive_bytes_total{device!="lo",%s}[5m]))`,
				},
				{
					series: "transmit",
					query:  `sum(rate(node_network_transmit_bytes_total{device!="lo",%s}[5m]))`,
				},
			},
		},
	},
}

// metricsResult is the envelope the metrics widget renders and an agent reads.
// Timestamps are RFC 3339 rather than epoch seconds: a model reads this as text
// as often as a chart plots it.
type metricsResult struct {
	Target string         `json:"target"`
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Metric string         `json:"metric"`
	Unit   string         `json:"unit"`
	Range  string         `json:"range"`
	Series []metricSeries `json:"series"`
}

type metricSeries struct {
	Name   string        `json:"name"`
	Points []metricPoint `json:"points"`
}

type metricPoint struct {
	Time  string  `json:"time"`
	Value float64 `json:"value"`
}

// toolGetMetrics charts one metric for one service or node. The target is
// resolved against the cache before anything is queried, and the query is
// built from what the cache holds, never from the caller's string — otherwise
// an agent could smuggle a label selector of its own through the id.
func (s *Server) toolGetMetrics(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	target, err := req.RequireString("target")
	if err != nil {
		return "", err
	}

	metric := req.GetString("metric", metricCPU)
	rangeKey := req.GetString("range", defaultMetricRange)

	// A ranking and a single-resource chart differ in what they aggregate by,
	// not just in what they select, so they are separate query catalogues and
	// the branch happens before either is consulted.
	if top := req.GetInt("top", 0); top > 0 || target == metricTargetCluster {
		return s.rankMetrics(ctx, req, target, metric, rangeKey, top)
	}

	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}

	metrics, ok := metricCatalog[target]
	if !ok {
		return "", fmt.Errorf(
			"unknown target %q; expected %q, %q or %q",
			target, metricTargetService, metricTargetNode, metricTargetCluster,
		)
	}

	spec, ok := metrics[metric]
	if !ok {
		return "", fmt.Errorf("unknown metric %q; expected one of %v", metric, metricNames(metrics))
	}

	window, ok := metricRanges[rangeKey]
	if !ok {
		return "", fmt.Errorf("unknown range %q; expected one of %v", rangeKey, rangeNames())
	}

	if s.prom == nil {
		return "", errors.New(
			"metrics are unavailable: this Cetacean has no Prometheus configured (CETACEAN_PROMETHEUS_URL)",
		)
	}

	var name, selector string

	switch target {
	case metricTargetService:
		name, selector, err = s.serviceMetricSelector(ctx, id)
	default:
		name, selector, err = s.nodeMetricSelector(ctx, id)
	}

	if err != nil {
		return "", err
	}

	end := time.Now()
	start := end.Add(-window.window)

	series := make([]metricSeries, 0, len(spec.queries))

	for _, query := range spec.queries {
		points, err := s.queryMetricSeries(
			ctx,
			fmt.Sprintf(query.query, selector),
			start,
			end,
			window.step,
		)
		if err != nil {
			return "", err
		}

		series = append(series, metricSeries{Name: query.series, Points: points})
	}

	if err := s.requireExporter(ctx, target, series, start, end, window.step); err != nil {
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

// requireExporter reports metrics as unavailable when nothing came back and
// the exporter behind this target is not reporting at all. A probe failure is
// not an error: failing the call because the explanation could not be fetched
// would turn a degraded answer into no answer.
func (s *Server) requireExporter(
	ctx context.Context,
	target string,
	series []metricSeries,
	start, end time.Time,
	step time.Duration,
) error {
	for _, one := range series {
		if len(one.Points) > 0 {
			return nil
		}
	}

	probe, ok := exporterProbes[target]
	if !ok {
		return nil
	}

	points, err := s.queryMetricSeries(ctx, probe.query, start, end, step)
	if err != nil {
		return nil //nolint:nilerr // a probe failure explains nothing; see above
	}

	if len(points) > 0 {
		return nil
	}

	return fmt.Errorf("metrics are unavailable: %s", probe.missing)
}

// queryMetricSeries runs one query and flattens what comes back. Every
// catalogue query aggregates to a single series; if Prometheus returns several
// anyway, their points are merged in time order rather than dropping all but
// the first.
func (s *Server) queryMetricSeries(
	ctx context.Context,
	query string,
	start, end time.Time,
	step time.Duration,
) ([]metricPoint, error) {
	results, err := s.prom.RangeQuery(
		ctx,
		query,
		strconv.FormatInt(start.Unix(), 10),
		strconv.FormatInt(end.Unix(), 10),
		strconv.Itoa(int(step.Seconds()))+"s",
	)
	if err != nil {
		return nil, fmt.Errorf("query metrics: %w", err)
	}

	points := make([]metricPoint, 0, len(results))

	for _, result := range results {
		for _, point := range result.Points {
			points = append(points, metricPoint{
				Time:  promTime(point.Timestamp),
				Value: point.Value,
			})
		}
	}

	slices.SortStableFunc(points, func(a, b metricPoint) int {
		return strings.Compare(a.Time, b.Time)
	})

	return points, nil
}

// serviceMetricSelector resolves a service by ID or name and returns the label
// selector matching its containers.
func (s *Server) serviceMetricSelector(ctx context.Context, id string) (string, string, error) {
	service, ok, err := s.cache.ResolveService(id)
	if err != nil {
		return "", "", err
	}

	if !ok {
		return "", "", fmt.Errorf("service %q not found", id)
	}

	if err := s.checkRead(ctx, "service", service.Spec.Name); err != nil {
		return "", "", err
	}

	return service.Spec.Name,
		fmt.Sprintf(`%s="%s"`, swarmServiceLabel, escapePromQLValue(service.Spec.Name)),
		nil
}

// nodeMetricSelector resolves a node by ID or hostname and returns the
// selector matching its node-exporter instance.
func (s *Server) nodeMetricSelector(ctx context.Context, id string) (string, string, error) {
	node, ok, err := s.cache.ResolveNode(id)
	if err != nil {
		return "", "", err
	}

	if !ok {
		return "", "", fmt.Errorf("node %q not found", id)
	}

	if err := s.checkRead(ctx, "node", nodeACLName(node)); err != nil {
		return "", "", err
	}

	selector := instanceSelector(node)
	if selector == "" {
		return "", "", fmt.Errorf(
			"node %q has neither an address nor a hostname to match a node-exporter instance",
			id,
		)
	}

	return nodeACLName(node), selector, nil
}

// instanceSelector matches a node against node-exporter's `instance` label,
// which is host:port and known to neither Docker nor Cetacean. The node's
// address is the reliable half; its hostname, which the exporter may report
// fully qualified, is the fallback.
func instanceSelector(node swarm.Node) string {
	if address := node.Status.Addr; address != "" {
		return fmt.Sprintf(`instance=~"%s:.*"`, promQLRegexValue(address))
	}

	if hostname := node.Description.Hostname; hostname != "" {
		return fmt.Sprintf(`instance=~"%s(\\..+)?:.*"`, promQLRegexValue(hostname))
	}

	return ""
}

// escapePromQLValue escapes a value for a PromQL string literal. Every value
// that reaches it comes from the cache rather than from the caller, so this is
// the second line of defence rather than the first — but a service named with
// a quote would otherwise produce a query that does not parse.
func escapePromQLValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value)
}

// promQLRegexValue quotes a value for a PromQL regex matcher. String escaping
// alone is not enough, since `=~` compiles what it is given: "api.v2" would
// also match "apixv2", reaching past the caller's grants. Regex-quote first
// and escape after, since QuoteMeta's backslashes are themselves escapes.
func promQLRegexValue(value string) string {
	return escapePromQLValue(regexp.QuoteMeta(value))
}

func metricNames(metrics map[string]metricSpec) []string {
	names := make([]string, 0, len(metrics))
	for name := range metrics {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// rangeNames lists the presets `range` accepts — the advertised enum and the
// list both "unknown range" errors print. Shortest window first, the order a
// reader expects, where sorting the keys as strings would put 24h before 6h.
func rangeNames() []string {
	return slices.SortedFunc(maps.Keys(metricRanges), func(a, b string) int {
		return cmp.Compare(metricRanges[a].window, metricRanges[b].window)
	})
}

// promTime renders a Prometheus timestamp — a float of Unix seconds — as the
// RFC 3339 string a metric point carries. Shared by the single-resource read
// and the ranking, so the two cannot format one instant differently.
func promTime(timestamp float64) string {
	seconds := int64(timestamp)
	fraction := timestamp - float64(seconds)

	return time.Unix(seconds, int64(fraction*1e9)).UTC().Format(time.RFC3339)
}
