package mcp

import (
	"context"
	"errors"
	"fmt"
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

// exporterProbe answers "is anything collecting for this target at all?".
//
// It is asked only when every series came back empty, which is ambiguous on
// its own: a service that is genuinely idle and a cluster with no cAdvisor
// both produce no points. The tool promises to report unavailability rather
// than return empty series, and right_size_service tells the model to stop on
// exactly that signal — an empty series would otherwise read as measured zero
// usage and invite shrinking a service to nothing.
//
// Presence of the exporter's own metric family is the test, never a `job`
// label: the job name belongs to whoever wrote the Prometheus config, which
// HandleMonitoringStatus makes the same argument about for node-exporter. The
// service probe requires the Swarm service label too, because a cAdvisor that
// reports only its own root cgroup — cgroup v2 without a host cgroup
// namespace does exactly that — emits container metrics that can never be
// attributed to a service.
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

// metricCatalog holds every query get_metrics can run.
//
// These mirror the queries the dashboard composes in
// frontend/src/hooks/useServiceMetrics.ts, useNodeMetrics.ts and NodeDetail —
// deliberately a second copy rather than a shared one, since the dashboard
// builds its PromQL in the browser and there is no server-side query layer to
// share. Keep the two in step: a metric that reads differently here than on the
// dashboard is worse than one that is missing.
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
//
// Timestamps are RFC 3339 rather than epoch seconds: this is a tool result a
// model reads as text as often as a chart plots it, and "2026-09-01T09:00:00Z"
// needs no explaining where 1788254400 does.
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

// toolGetMetrics charts one metric for one service or node.
//
// The target is resolved against the cache *before* anything is queried, and
// the query is built from what the cache holds — never from the caller's
// string. That is what makes the ACL check meaningful: an agent cannot name a
// service it may not read, and cannot smuggle a label selector of its own
// through the id either.
func (s *Server) toolGetMetrics(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	target, err := req.RequireString("target")
	if err != nil {
		return "", err
	}

	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}

	metric := req.GetString("metric", metricCPU)
	rangeKey := req.GetString("range", defaultMetricRange)

	metrics, ok := metricCatalog[target]
	if !ok {
		return "", fmt.Errorf(
			"unknown target %q; expected %q or %q",
			target, metricTargetService, metricTargetNode,
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
// the exporter behind this target is not reporting at all.
//
// A probe failure is not an error: the point is to explain an empty result,
// and failing the whole call because the explanation could not be fetched
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

// queryMetricSeries runs one query and flattens what comes back.
//
// Every query in the catalogue aggregates to a single series, so a well-formed
// answer has one element; if Prometheus returns several anyway (a recording
// rule that did not aggregate, say) their points are merged in time order
// rather than silently dropping all but the first.
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
			seconds, fraction := int64(
				point.Timestamp,
			), point.Timestamp-float64(
				int64(point.Timestamp),
			)

			points = append(points, metricPoint{
				Time:  time.Unix(seconds, int64(fraction*1e9)).UTC().Format(time.RFC3339),
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
// which is host:port and known to neither Docker nor Cetacean. Mirrors
// buildInstanceFilter in frontend/src/lib/prometheusParser.ts: the node's
// address is the reliable half of the pair, and its hostname — which the
// exporter may report fully qualified — is the fallback.
func instanceSelector(node swarm.Node) string {
	if address := node.Status.Addr; address != "" {
		return fmt.Sprintf(`instance=~"%s:.*"`, escapePromQLValue(address))
	}

	if hostname := node.Description.Hostname; hostname != "" {
		return fmt.Sprintf(`instance=~"%s(\\..+)?:.*"`, escapePromQLValue(hostname))
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

func metricNames(metrics map[string]metricSpec) []string {
	names := make([]string, 0, len(metrics))
	for name := range metrics {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

func rangeNames() []string {
	names := make([]string, 0, len(metricRanges))
	for name := range metricRanges {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}
