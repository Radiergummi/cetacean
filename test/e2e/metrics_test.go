//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives the metrics domain against a real Prometheus holding
// generated history. It reserves port 19020 (see README.md's reserved-ports
// table).
//
// Everything the lane asserts is a number it put there. The series below are
// counters rising by a fixed step every harness.SeedInterval, so `rate()` over
// the [5m] window every query in the product uses yields an exact value rather
// than one within a tolerance — which is what lets a case say "the CPU panel
// reads 50%" instead of "the CPU panel reads something".
//
// Until this lane, internal/prometheus was covered end-to-end only in its
// "Prometheus is not configured, answer 503" branch.

const metricsPort = 19020

// The seeded utilisation. Distinct on purpose: were they equal, a query
// reading the wrong metric family would still produce the expected number.
const (
	seededNodeCPUPercent    = 25.0
	seededNodeMemoryPercent = 40.0
	seededNodeDiskPercent   = 60.0

	seededMemoryTotal = 10 * 1024 * 1024 * 1024 // 10 GiB
	seededMemoryAvail = 6 * 1024 * 1024 * 1024  // 6 GiB, so 4 GiB used
	seededDiskTotal   = 100 * 1024 * 1024 * 1024
	seededDiskAvail   = 40 * 1024 * 1024 * 1024 // 60 GiB used
)

// Per-service CPU, as the percentage `sum(rate(...)) * 100` yields, and
// per-service memory in bytes.
var seededServiceCPU = map[string]float64{
	"shop_web":                50,
	"shop_lonely":             10,
	fixtures.CrashLoopService: 2,
	"platform_agent":          30,
}

// Memory deliberately ranks the services in the exact reverse of CPU. A
// ranking that read the wrong metric would otherwise still come back in the
// right order, and the cases below assert an order.
var seededServiceMemory = map[string]float64{
	fixtures.CrashLoopService: 512 * 1024 * 1024,
	"shop_lonely":             256 * 1024 * 1024,
	"platform_agent":          128 * 1024 * 1024,
	"shop_web":                64 * 1024 * 1024,
}

// Which stack each service belongs to, for the per-stack rollup.
var seededServiceStack = map[string]string{
	"shop_web":                fixtures.StackShop,
	"shop_lonely":             fixtures.StackShop,
	fixtures.CrashLoopService: fixtures.StackShop,
	"platform_agent":          fixtures.StackPlatform,
}

const (
	seededNetworkReceive  = 1000.0 // bytes/second, shop_web
	seededNetworkTransmit = 500.0

	seededNodeNetworkReceive  = 2000.0 // bytes/second, on eth0
	seededNodeNetworkTransmit = 750.0
)

// ─── the seed ───────────────────────────────────────────────────────────

// seededMetrics builds the exposition for a node reachable at `address`. The
// address is the half of node-exporter's `instance` label Cetacean matches on
// (internal/mcp/metrics.go's instanceSelector), and it is assigned by Docker,
// so the series cannot be written down ahead of the cluster.
func seededMetrics(address, hostname string) []harness.Series {
	nodeInstance := address + ":9100"
	cadvisorInstance := address + ":8080"

	// A counter rising by `perSecond` every interval.
	counter := func(name string, labels map[string]string, perSecond float64) harness.Series {
		return harness.Series{
			Name:   name,
			Labels: labels,
			Start:  0,
			Step:   perSecond * harness.SeedInterval.Seconds(),
		}
	}

	gauge := func(name string, labels map[string]string, value float64) harness.Series {
		return harness.Series{Name: name, Labels: labels, Start: value}
	}

	nodeLabels := map[string]string{"instance": nodeInstance, "job": "node-exporter"}

	series := []harness.Series{
		// What /metrics/status detects each exporter by: node-exporter's own
		// metric family, and cAdvisor's `up` under the job name the docs
		// require it be given.
		gauge("node_uname_info", map[string]string{
			"instance": nodeInstance, "job": "node-exporter", "nodename": hostname,
		}, 1),
		gauge("up", map[string]string{"instance": nodeInstance, "job": "node-exporter"}, 1),
		gauge("up", map[string]string{"instance": cadvisorInstance, "job": "cadvisor"}, 1),

		// 25% busy: three quarters idle, one quarter user.
		counter("node_cpu_seconds_total", withLabels(nodeLabels, map[string]string{
			"cpu": "0", "mode": "idle",
		}), 1-seededNodeCPUPercent/100),
		counter("node_cpu_seconds_total", withLabels(nodeLabels, map[string]string{
			"cpu": "0", "mode": "user",
		}), seededNodeCPUPercent/100),

		gauge("node_memory_MemTotal_bytes", nodeLabels, seededMemoryTotal),
		gauge("node_memory_MemAvailable_bytes", nodeLabels, seededMemoryAvail),

		gauge("node_filesystem_size_bytes", withLabels(nodeLabels, map[string]string{
			"mountpoint": "/", "fstype": "ext4",
		}), seededDiskTotal),
		gauge("node_filesystem_avail_bytes", withLabels(nodeLabels, map[string]string{
			"mountpoint": "/", "fstype": "ext4",
		}), seededDiskAvail),

		counter("node_network_receive_bytes_total", withLabels(nodeLabels, map[string]string{
			"device": "eth0",
		}), seededNodeNetworkReceive),
		counter("node_network_transmit_bytes_total", withLabels(nodeLabels, map[string]string{
			"device": "eth0",
		}), seededNodeNetworkTransmit),
		// Loopback, which every node query excludes with device!="lo". Seeded
		// large enough that a query forgetting the exclusion reads wrong.
		counter("node_network_receive_bytes_total", withLabels(nodeLabels, map[string]string{
			"device": "lo",
		}), 9_000_000),
	}

	for name, cpu := range seededServiceCPU {
		labels := map[string]string{
			"instance": cadvisorInstance,
			"job":      "cadvisor",
			// The three labels every container query in the product selects
			// on. The id is only ever tested for emptiness, so it need not be
			// the service's real one.
			"container_label_com_docker_swarm_service_name": name,
			"container_label_com_docker_swarm_service_id":   "seeded-" + name,
			"container_label_com_docker_stack_namespace":    seededServiceStack[name],
		}

		series = append(series,
			counter("container_cpu_usage_seconds_total", labels, cpu/100),
			gauge("container_memory_usage_bytes", labels, seededServiceMemory[name]),
		)

		if name == "shop_web" {
			series = append(series,
				counter("container_network_receive_bytes_total", labels, seededNetworkReceive),
				counter("container_network_transmit_bytes_total", labels, seededNetworkTransmit),
			)
		}
	}

	return series
}

func withLabels(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	maps.Copy(out, base)
	maps.Copy(out, extra)

	return out
}

// metricsCluster brings the environment up with the baseline deployed and
// reports the node's address, which is the half of node-exporter's `instance`
// label instanceSelector matches on — Docker assigns it, so no series can be
// written down ahead of it.
func metricsCluster(t *testing.T) (*harness.Env, string, string) {
	t.Helper()

	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	nodes, err := env.Docker.NodeList(t.Context(), swarm.NodeListOptions{})
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("expected one node in the fixture cluster, got %d", len(nodes))
	}

	address := nodes[0].Status.Addr
	if address == "" {
		t.Fatal(
			"the swarm node reports no address, and node-exporter's instance label is matched on it",
		)
	}

	return env, address, nodes[0].Description.Hostname
}

// seedAndStart writes the series and points a SUT at the Prometheus holding
// them. Each case seeds its own, so one case's series cannot reach the next.
func seedAndStart(
	t *testing.T,
	env *harness.Env,
	series []harness.Series,
	extraEnv map[string]string,
) (*sut.Process, time.Time) {
	t.Helper()

	seededAt := harness.SeedPrometheus(t, series)

	vars := map[string]string{
		"CETACEAN_AUTH_MODE":      "none",
		"CETACEAN_PROMETHEUS_URL": harness.PrometheusURL,
	}
	maps.Copy(vars, extraEnv)

	proc := sut.Start(t, sut.Config{
		Port:       metricsPort,
		DockerHost: env.DockerHost,
		Env:        vars,
	})

	return proc, seededAt
}

// startMetricsLane is the whole seed against the baseline cluster.
func startMetricsLane(t *testing.T) (*sut.Process, time.Time) {
	t.Helper()

	return startMetricsLaneWith(t, nil)
}

// startMetricsLaneWith seeds only the series `keep` accepts, for a case about
// what Cetacean reports when an exporter is missing.
func startMetricsLaneWith(t *testing.T, keep func(harness.Series) bool) (*sut.Process, time.Time) {
	t.Helper()

	env, address, hostname := metricsCluster(t)
	series := seededMetrics(address, hostname)

	if keep != nil {
		kept := make([]harness.Series, 0, len(series))
		for _, s := range series {
			if keep(s) {
				kept = append(kept, s)
			}
		}

		series = kept
	}

	return seedAndStart(t, env, series, nil)
}

// ─── the tests ──────────────────────────────────────────────────────────

// TestMetricsStatusDetectsBothExporters drives the guided-setup probe the
// dashboard's banner reads. Both exporters are present in the seed, so the
// healthy state is the one under test — the unconfigured state is covered in
// api_test.go, and was until now the only one.
func TestMetricsStatusDetectsBothExporters(t *testing.T) {
	proc, _ := startMetricsLane(t)

	// DetailResponse splices the payload's fields in beside the @-prefixed
	// keys rather than nesting it under a name of its own.
	var status struct {
		PrometheusConfigured bool   `json:"prometheusConfigured"`
		PrometheusReachable  bool   `json:"prometheusReachable"`
		Error                string `json:"error"`
		NodeExporter         *struct {
			Targets int `json:"targets"`
			Nodes   int `json:"nodes"`
		} `json:"nodeExporter"`
		Cadvisor *struct {
			Targets int `json:"targets"`
			Nodes   int `json:"nodes"`
		} `json:"cadvisor"`
	}

	metricsGet(t, proc, "/metrics/status", &status)

	if !status.PrometheusConfigured || !status.PrometheusReachable {
		t.Fatalf("configured = %v, reachable = %v, error = %q, want both true",
			status.PrometheusConfigured, status.PrometheusReachable, status.Error)
	}

	if status.NodeExporter == nil {
		t.Fatal("nodeExporter is null, and node_uname_info is in the seed")
	}

	if status.NodeExporter.Targets != 1 || status.NodeExporter.Nodes != 1 {
		t.Errorf("nodeExporter = %d targets over %d nodes, want 1 over 1",
			status.NodeExporter.Targets, status.NodeExporter.Nodes)
	}

	if status.Cadvisor == nil {
		t.Fatal(`cadvisor is null, and up{job="cadvisor"} is in the seed`)
	}

	if status.Cadvisor.Targets != 1 || status.Cadvisor.Nodes != 1 {
		t.Errorf("cadvisor = %d targets over %d nodes, want 1 over 1",
			status.Cadvisor.Targets, status.Cadvisor.Nodes)
	}
}

// TestMetricsStatusReportsCadvisorMissing covers the state a cluster is
// actually in while it is being set up, and the one the docs warn about: a
// cAdvisor scraped under any job name but `cadvisor` is not detected, because
// detection asks `up{job="cadvisor"}` rather than asking for the metric family
// the way the node-exporter half does. The container series are all still
// here, which is what makes the asymmetry visible — every chart on the page
// has data, and the banner still reports the exporter as missing.
func TestMetricsStatusReportsCadvisorMissing(t *testing.T) {
	proc, _ := startMetricsLaneWith(t, func(s harness.Series) bool {
		return s.Name != "up" || s.Labels["job"] != "cadvisor"
	})

	var status struct {
		PrometheusReachable bool `json:"prometheusReachable"`
		NodeExporter        *struct {
			Targets int `json:"targets"`
		} `json:"nodeExporter"`
		Cadvisor *struct {
			Targets int `json:"targets"`
		} `json:"cadvisor"`
	}

	metricsGet(t, proc, "/metrics/status", &status)

	if !status.PrometheusReachable {
		t.Fatal("prometheus is reachable, and only the cAdvisor `up` series was withheld")
	}

	if status.NodeExporter == nil || status.NodeExporter.Targets != 1 {
		t.Errorf(
			"nodeExporter = %+v, want 1 target — it is detected by its metric family",
			status.NodeExporter,
		)
	}

	if status.Cadvisor == nil {
		t.Fatal("cadvisor is null, want a zero-target report")
	}

	if status.Cadvisor.Targets != 0 {
		t.Errorf("cadvisor = %d targets, want 0", status.Cadvisor.Targets)
	}

	// The container metrics the banner has just called unavailable.
	result := promResult(t, proc, url.Values{"query": {
		`count(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name!=""})`,
	}})

	if len(result.Data.Result) != 1 {
		t.Fatal("no container series, and the seed withheld only `up`")
	}

	if got := sampleValue(t, result.Data.Result[0].Value); got != float64(len(seededServiceCPU)) {
		t.Errorf("%v container series are being scraped while cAdvisor reads as missing, want %d",
			got, len(seededServiceCPU))
	}
}

// TestClusterMetricsReportTheSeededUtilisation drives the three queries behind
// the cluster overview's utilisation bars, each of which composes a PromQL
// expression the product writes and nothing outside it had ever run.
func TestClusterMetricsReportTheSeededUtilisation(t *testing.T) {
	proc, _ := startMetricsLane(t)

	var got struct {
		CPU    metricValue `json:"cpu"`
		Memory metricValue `json:"memory"`
		Disk   metricValue `json:"disk"`
	}

	metricsGet(t, proc, "/cluster/metrics", &got)

	closeTo(t, "cpu percent", got.CPU.Percent, seededNodeCPUPercent)

	// Memory is reported as bytes used, read from Prometheus, over the total
	// the cache holds from Docker — so only the used half is seeded.
	closeTo(t, "memory used", got.Memory.Used, seededMemoryTotal-seededMemoryAvail)

	closeTo(t, "disk percent", got.Disk.Percent, seededNodeDiskPercent)
	closeTo(t, "disk total", got.Disk.Total, seededDiskTotal)
	closeTo(t, "disk used", got.Disk.Used, seededDiskTotal-seededDiskAvail)
}

// TestMetricsProxyServesInstantAndRangeQueries drives GET /metrics in both of
// its modes. The proxy is what every chart in the dashboard reads through, and
// which mode it takes is decided by the presence of start+end.
func TestMetricsProxyServesInstantAndRangeQueries(t *testing.T) {
	proc, seededAt := startMetricsLane(t)

	t.Run("instant", func(t *testing.T) {
		query := fmt.Sprintf(
			`sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name=%q}[5m])) * 100`,
			"shop_web",
		)

		result := promResult(t, proc, url.Values{"query": {query}})
		if result.Data.ResultType != "vector" {
			t.Fatalf("resultType = %q, want vector", result.Data.ResultType)
		}

		if len(result.Data.Result) != 1 {
			t.Fatalf("got %d series, want 1", len(result.Data.Result))
		}

		closeTo(
			t,
			"shop_web cpu",
			sampleValue(t, result.Data.Result[0].Value),
			seededServiceCPU["shop_web"],
		)
	})

	t.Run("range", func(t *testing.T) {
		const step = 60

		end := seededAt
		start := end.Add(-30 * time.Minute)

		result := promResult(t, proc, url.Values{
			"query": {
				`container_memory_usage_bytes{container_label_com_docker_swarm_service_name="shop_web"}`,
			},
			"start": {strconv.FormatInt(start.Unix(), 10)},
			"end":   {strconv.FormatInt(end.Unix(), 10)},
			"step":  {strconv.Itoa(step)},
		})

		if result.Data.ResultType != "matrix" {
			t.Fatalf("resultType = %q, want matrix", result.Data.ResultType)
		}

		if len(result.Data.Result) != 1 {
			t.Fatalf("got %d series, want 1", len(result.Data.Result))
		}

		values := result.Data.Result[0].Values
		if want := 31; len(values) != want {
			t.Errorf(
				"got %d points over 30 minutes at a %ds step, want %d",
				len(values),
				step,
				want,
			)
		}

		for _, point := range values {
			closeTo(t, "shop_web memory", sampleValue(t, point), seededServiceMemory["shop_web"])
		}
	})
}

// TestMetricsLabelsServeMetadata drives /metrics/labels and
// /metrics/labels/{name}, the two endpoints behind the metrics console's
// selectors. Both proxy Prometheus's metadata API and neither had been driven.
func TestMetricsLabelsServeMetadata(t *testing.T) {
	proc, _ := startMetricsLane(t)

	var names struct {
		Data []string `json:"data"`
	}

	metricsGet(t, proc, "/metrics/labels", &names)

	for _, want := range []string{"__name__", "instance", "job"} {
		if !slices.Contains(names.Data, want) {
			t.Errorf("label %q is missing from /metrics/labels: %v", want, names.Data)
		}
	}

	var values struct {
		Data []string `json:"data"`
	}

	metricsGet(t, proc, "/metrics/labels/job", &values)

	for _, want := range []string{"cadvisor", "node-exporter"} {
		if !slices.Contains(values.Data, want) {
			t.Errorf("job %q is missing from /metrics/labels/job: %v", want, values.Data)
		}
	}
}

// TestStackSummaryRollsUpSeededMemory drives the per-stack memory rollup,
// which groups container series by the stack-namespace label rather than by
// anything Docker reports.
func TestStackSummaryRollsUpSeededMemory(t *testing.T) {
	proc, _ := startMetricsLane(t)

	want := map[string]float64{}
	for service, memory := range seededServiceMemory {
		want[seededServiceStack[service]] += memory
	}

	var body struct {
		Items []struct {
			Name   string  `json:"name"`
			Memory float64 `json:"memoryUsageBytes"`
		} `json:"items"`
	}

	metricsGet(t, proc, "/stacks/summary", &body)

	seen := map[string]bool{}

	for _, item := range body.Items {
		expected, ok := want[item.Name]
		if !ok {
			continue
		}

		seen[item.Name] = true

		closeTo(t, item.Name+" memory", item.Memory, expected)
	}

	// Without this the case would pass on a summary holding no stack it knows
	// about, which is every way this could go wrong at once.
	for name := range want {
		if !seen[name] {
			t.Errorf("stack %q is missing from the summary", name)
		}
	}
}

// ─── plumbing ───────────────────────────────────────────────────────────

type metricValue struct {
	Used    float64 `json:"used"`
	Total   float64 `json:"total"`
	Percent float64 `json:"percent"`
}

type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
			Values [][]any           `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func promResult(t *testing.T, proc *sut.Process, query url.Values) promResponse {
	t.Helper()

	var out promResponse
	metricsGet(t, proc, "/metrics?"+query.Encode(), &out)

	if out.Status != "success" {
		t.Fatalf("prometheus status = %q, want success", out.Status)
	}

	return out
}

// sampleValue reads the value out of Prometheus's [timestamp, "value"] pair.
func sampleValue(t *testing.T, pair []any) float64 {
	t.Helper()

	if len(pair) != 2 {
		t.Fatalf("sample = %v, want a [timestamp, value] pair", pair)
	}

	raw, ok := pair[1].(string)
	if !ok {
		t.Fatalf("sample value = %T, want a string", pair[1])
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("parse sample %q: %v", raw, err)
	}

	return value
}

func metricsGet(t *testing.T, proc *sut.Process, path string, into any) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proc.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200", path, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// closeTo compares against a seeded value. The tolerance is for float
// arithmetic through PromQL and JSON, not for the sampling — the seed makes
// every number below exact.
func closeTo(t *testing.T, what string, got, want float64) {
	t.Helper()

	if math.Abs(got-want) > math.Max(1, math.Abs(want)*1e-6) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// ─── sizing recommendations ─────────────────────────────────────────────

// The two throwaway services the sizing cases are seeded against, and the
// numbers the checker is expected to derive from them. CPU is a percentage of
// one core, which is the unit Prometheus reports and the checker converts from.
const (
	hotCPULimit    = 100_000_000 // 0.1 cores, so a 10% limit
	hotCPUUsage    = 9.7         // 97% of that limit: at-limit, critical
	hotMemoryLimit = 128 * 1024 * 1024
	hotMemoryUsage = 126 * 1024 * 1024 // 98% of the limit

	roomyCPULimit          = 2_000_000_000
	roomyCPUReservation    = 1_000_000_000 // a whole core reserved
	roomyCPUUsage          = 5.0           // 5% of it: over-provisioned
	roomyMemoryLimit       = 1024 * 1024 * 1024
	roomyMemoryReservation = 512 * 1024 * 1024
	roomyMemoryUsage       = 32 * 1024 * 1024 // 6% of the reservation
)

// TestSizingRecommendationsReadTheSeededUsage drives the recommendation engine's
// only Prometheus-dependent checker end to end. It is the reason the metrics
// domain is worth seeding at all: the sizing rules turn measured usage into
// advice a user is invited to apply with one click, and nothing had ever run
// them against a Prometheus.
//
// Two services, chosen to reach both halves of the rules: one pinned just under
// its limits, one given far more than it uses.
func TestSizingRecommendationsReadTheSeededUsage(t *testing.T) {
	env, address, hostname := metricsCluster(t)

	stack := fixtures.DeployStack(t, env, "sizing", []fixtures.ServiceSpec{
		{
			Name:     "hot",
			Replicas: 1,
			Command:  []string{"sleep infinity"},
			Resources: &fixtures.ResourceSpec{
				CPULimit:    hotCPULimit,
				MemoryLimit: hotMemoryLimit,
			},
		},
		{
			Name:     "roomy",
			Replicas: 1,
			Command:  []string{"sleep infinity"},
			Resources: &fixtures.ResourceSpec{
				CPULimit:          roomyCPULimit,
				MemoryLimit:       roomyMemoryLimit,
				CPUReservation:    roomyCPUReservation,
				MemoryReservation: roomyMemoryReservation,
			},
		},
	})

	hot := stack + "_hot"
	roomy := stack + "_roomy"

	series := append(
		seededMetrics(address, hostname),
		serviceSeries(address, hot, stack, hotCPUUsage, hotMemoryUsage)...,
	)
	series = append(
		series,
		serviceSeries(address, roomy, stack, roomyCPUUsage, roomyMemoryUsage)...)

	proc, _ := seedAndStart(t, env, series, nil)

	// The engine forces every checker once at startup and then leaves the
	// sizing checker alone for five minutes, so the bound below is what makes
	// this a test rather than a wait: findings that are not here within it are
	// not coming until long after, and the startup tick is held behind the
	// first cache sync precisely so they are here.
	want := map[string]string{
		hot + "/cpu":      "at-limit",
		hot + "/memory":   "at-limit",
		roomy + "/cpu":    "over-provisioned",
		roomy + "/memory": "over-provisioned",
	}

	var last []recommendation

	started := time.Now()
	deadline := started.Add(7 * time.Minute)
	for time.Now().Before(deadline) {
		last = sizingRecommendations(t, proc)

		if covers(last, want) {
			t.Logf(
				"DIAGNOSTIC: sizing recommendations appeared after %s",
				time.Since(started).Round(time.Second),
			)
			return
		}

		time.Sleep(2 * time.Second)
	}

	for key, category := range want {
		if !hasRecommendation(last, key, category) {
			t.Errorf("no %s recommendation for %s", category, key)
		}
	}

	t.Logf("recommendations seen: %+v", last)
}

// serviceSeries is the cAdvisor half of the seed for one service: the three
// labels every container query in the product selects on, a CPU counter rising
// at the given percentage of a core, and a flat memory gauge.
func serviceSeries(
	address, service, stack string,
	cpuPercent, memoryBytes float64,
) []harness.Series {
	labels := map[string]string{
		"instance": address + ":8080",
		"job":      "cadvisor",
		"container_label_com_docker_swarm_service_name": service,
		"container_label_com_docker_swarm_service_id":   "seeded-" + service,
		"container_label_com_docker_stack_namespace":    stack,
	}

	return []harness.Series{
		{
			Name:   "container_cpu_usage_seconds_total",
			Labels: labels,
			Step:   cpuPercent / 100 * harness.SeedInterval.Seconds(),
		},
		{Name: "container_memory_usage_bytes", Labels: labels, Start: memoryBytes},
	}
}

type recommendation struct {
	Category   string  `json:"category"`
	Severity   string  `json:"severity"`
	TargetName string  `json:"targetName"`
	Resource   string  `json:"resource"`
	Current    float64 `json:"current"`
	Configured float64 `json:"configured"`
	Suggested  float64 `json:"suggested"`
	Message    string  `json:"message"`
}

func sizingRecommendations(t *testing.T, proc *sut.Process) []recommendation {
	t.Helper()

	var body struct {
		Items []recommendation `json:"items"`
	}

	metricsGet(t, proc, "/recommendations?limit=200", &body)

	return body.Items
}

func hasRecommendation(recs []recommendation, key, category string) bool {
	for _, rec := range recs {
		if rec.TargetName+"/"+rec.Resource == key && rec.Category == category {
			return true
		}
	}

	return false
}

func covers(recs []recommendation, want map[string]string) bool {
	for key, category := range want {
		if !hasRecommendation(recs, key, category) {
			return false
		}
	}

	return true
}

// ─── the MCP transport over the same seed ───────────────────────────────

// TestMCPGetMetricsReadsTheSameSeed drives `get_metrics`, the tool behind the
// metrics widget. Its queries are deliberately a second copy of the ones the
// dashboard composes in the browser — there is no shared query layer — so the
// value of driving them here is that a copy which has drifted answers with a
// different number than the REST side does against the same seed.
//
// It also covers what the tool does with an id: the target is resolved against
// the cache and the *cached name* is what reaches the query, so a service is
// addressable by either.
func TestMCPGetMetricsReadsTheSameSeed(t *testing.T) {
	env, address, hostname := metricsCluster(t)

	proc, _ := seedAndStart(t, env, seededMetrics(address, hostname), map[string]string{
		"CETACEAN_MCP": "true",
	})

	t.Run("service cpu", func(t *testing.T) {
		series := metricsToolSeries(t, proc, map[string]any{
			"target": "service", "id": "shop_web", "metric": "cpu", "range": "1h",
		})

		closeTo(t, "shop_web cpu over MCP", latest(t, series["cpu"]), seededServiceCPU["shop_web"])
	})

	t.Run("service memory", func(t *testing.T) {
		series := metricsToolSeries(t, proc, map[string]any{
			"target": "service", "id": "shop_web", "metric": "memory", "range": "1h",
		})

		closeTo(
			t,
			"shop_web memory over MCP",
			latest(t, series["memory"]),
			seededServiceMemory["shop_web"],
		)
	})

	t.Run("service network", func(t *testing.T) {
		series := metricsToolSeries(t, proc, map[string]any{
			"target": "service", "id": "shop_web", "metric": "network", "range": "1h",
		})

		closeTo(t, "shop_web receive", latest(t, series["receive"]), seededNetworkReceive)
		closeTo(t, "shop_web transmit", latest(t, series["transmit"]), seededNetworkTransmit)
	})

	// The node is addressed by ID, and matched to node-exporter's instance
	// label by the address Docker assigned it — the one piece of this seed
	// that could not be written down in advance.
	t.Run("node cpu", func(t *testing.T) {
		series := metricsToolSeries(t, proc, map[string]any{
			"target": "node", "id": hostname, "metric": "cpu", "range": "1h",
		})

		closeTo(t, "node cpu over MCP", latest(t, series["cpu"]), seededNodeCPUPercent)
	})

	t.Run("node memory", func(t *testing.T) {
		series := metricsToolSeries(t, proc, map[string]any{
			"target": "node", "id": hostname, "metric": "memory", "range": "1h",
		})

		closeTo(t, "node memory over MCP", latest(t, series["memory"]), seededNodeMemoryPercent)
	})
}

// TestMCPGetMetricsReportsAMissingExporter covers the answer the tool gives
// when every series comes back empty. An empty series alone cannot tell an
// idle resource from a cluster with no cAdvisor, and `right_size_service`
// instructs the model to stop on exactly that signal — so the tool probes for
// the exporter rather than charting zeros.
func TestMCPGetMetricsReportsAMissingExporter(t *testing.T) {
	env, address, hostname := metricsCluster(t)

	// Every node series, and none of the container ones: node-exporter is
	// reporting and cAdvisor is not.
	var series []harness.Series
	for _, s := range seededMetrics(address, hostname) {
		if !strings.HasPrefix(s.Name, "container_") {
			series = append(series, s)
		}
	}

	proc, _ := seedAndStart(t, env, series, map[string]string{"CETACEAN_MCP": "true"})

	result := mcpCall(t, proc, "tools/call", map[string]any{
		"name": "get_metrics",
		"arguments": map[string]any{
			"target": "service", "id": "shop_web", "metric": "cpu", "range": "1h",
		},
	})

	if !strings.Contains(strings.ToLower(string(result)), "cadvisor") {
		t.Errorf("get_metrics answered %s, want it to name the missing exporter", result)
	}

	// The node half still answers, which is what makes the message a report
	// about cAdvisor rather than about Prometheus.
	node := metricsToolSeries(t, proc, map[string]any{
		"target": "node", "id": hostname, "metric": "cpu", "range": "1h",
	})

	closeTo(t, "node cpu", latest(t, node["cpu"]), seededNodeCPUPercent)
}

// metricsToolSeries calls get_metrics and returns its points by series name.
func metricsToolSeries(t *testing.T, proc *sut.Process, args map[string]any) map[string][]float64 {
	t.Helper()

	raw := mcpCall(t, proc, "tools/call", map[string]any{
		"name":      "get_metrics",
		"arguments": args,
	})

	var result struct {
		IsError           bool `json:"isError"`
		StructuredContent struct {
			Series []struct {
				Name   string `json:"name"`
				Points []struct {
					Value float64 `json:"value"`
				} `json:"points"`
			} `json:"series"`
		} `json:"structuredContent"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode get_metrics: %v\n%s", err, raw)
	}

	if result.IsError {
		t.Fatalf("get_metrics(%v) failed: %s", args, raw)
	}

	out := map[string][]float64{}
	for _, series := range result.StructuredContent.Series {
		values := make([]float64, 0, len(series.Points))
		for _, point := range series.Points {
			values = append(values, point.Value)
		}

		out[series.Name] = values
	}

	if len(out) == 0 {
		t.Fatalf("get_metrics(%v) returned no series: %s", args, raw)
	}

	return out
}

// latest is the most recent point of a series, which is the number a panel
// shows as the current value.
func latest(t *testing.T, points []float64) float64 {
	t.Helper()

	if len(points) == 0 {
		t.Fatal("series has no points")
	}

	return points[len(points)-1]
}

// ─── the ranking form of get_metrics ────────────────────────────────────

// rankedNames is the ranking's members in the order it returned them.
func rankedNames(t *testing.T, proc *sut.Process, args map[string]any) []string {
	t.Helper()

	raw := mcpCall(t, proc, "tools/call", map[string]any{
		"name":      "get_metrics",
		"arguments": args,
	})

	var result struct {
		IsError           bool `json:"isError"`
		StructuredContent struct {
			Series []struct {
				Name   string `json:"name"`
				Points []struct {
					Value float64 `json:"value"`
				} `json:"points"`
			} `json:"series"`
		} `json:"structuredContent"`
	}

	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode get_metrics: %v\n%s", err, raw)
	}

	if result.IsError {
		t.Fatalf("get_metrics(%v) failed: %s", args, raw)
	}

	// Prometheus returns a matrix in no particular order — topk decides
	// membership, not order — so the ranking is by the value each member
	// actually reached, which is what a reader of the chart sees.
	type member struct {
		name  string
		value float64
	}

	members := make([]member, 0, len(result.StructuredContent.Series))

	for _, series := range result.StructuredContent.Series {
		if len(series.Points) == 0 {
			t.Errorf("ranked series %q came back with no points", series.Name)

			continue
		}

		members = append(members, member{
			name:  series.Name,
			value: series.Points[len(series.Points)-1].Value,
		})
	}

	slices.SortFunc(members, func(a, b member) int {
		switch {
		case a.value > b.value:
			return -1
		case a.value < b.value:
			return 1
		default:
			return 0
		}
	})

	names := make([]string, 0, len(members))
	for _, m := range members {
		names = append(names, m.name)
	}

	return names
}

// TestMCPRankMetricsOrdersTheSeededMembers drives the ranking form of
// get_metrics, which has its own PromQL catalog — separate from the charting
// one because the aggregation differs — and had never been executed against a
// Prometheus at all.
//
// The seed ranks the services in one order by CPU and the exact reverse by
// memory, so a ranking that read the wrong metric could not come back in the
// right order by luck.
func TestMCPRankMetricsOrdersTheSeededMembers(t *testing.T) {
	env, address, hostname := metricsCluster(t)

	proc, _ := seedAndStart(t, env, seededMetrics(address, hostname), map[string]string{
		"CETACEAN_MCP": "true",
	})

	t.Run("services by cpu", func(t *testing.T) {
		got := rankedNames(t, proc, map[string]any{
			"target": "cluster", "by": "service", "metric": "cpu", "range": "1h", "top": 3,
		})

		want := []string{"shop_web", "platform_agent", "shop_lonely"}
		if !slices.Equal(got, want) {
			t.Errorf("top 3 services by CPU = %v, want %v", got, want)
		}
	})

	t.Run("services by memory", func(t *testing.T) {
		got := rankedNames(t, proc, map[string]any{
			"target": "cluster", "by": "service", "metric": "memory", "range": "1h", "top": 3,
		})

		want := []string{fixtures.CrashLoopService, "shop_lonely", "platform_agent"}
		if !slices.Equal(got, want) {
			t.Errorf("top 3 services by memory = %v, want %v", got, want)
		}
	})

	// `top` is what bounds the answer, and a ranking that ignored it would
	// hand a model every service in the cluster.
	t.Run("top bounds the ranking", func(t *testing.T) {
		got := rankedNames(t, proc, map[string]any{
			"target": "cluster", "by": "service", "metric": "cpu", "range": "1h", "top": 1,
		})

		if !slices.Equal(got, []string{"shop_web"}) {
			t.Errorf("top 1 by CPU = %v, want [shop_web]", got)
		}
	})

	// A node ranks under the name the cluster calls it, not under the
	// `instance` label Prometheus knows it by — nameRankedSeries resolves the
	// host half back through the cache, and an unresolved instance would show
	// up here as "10.0.0.2:9100".
	t.Run("nodes are named as the cluster names them", func(t *testing.T) {
		got := rankedNames(t, proc, map[string]any{
			"target": "cluster", "by": "node", "metric": "cpu", "range": "1h", "top": 5,
		})

		if !slices.Equal(got, []string{hostname}) {
			t.Errorf("nodes ranked by CPU = %v, want [%s]", got, hostname)
		}
	})

	// The loopback series is seeded far above the real one, so a query that
	// dropped its device!="lo" exclusion reads several times too high.
	t.Run("node network excludes loopback", func(t *testing.T) {
		series := metricsToolSeries(t, proc, map[string]any{
			"target": "node", "id": hostname, "metric": "network", "range": "1h",
		})

		closeTo(t, "node receive", latest(t, series["receive"]), seededNodeNetworkReceive)
		closeTo(t, "node transmit", latest(t, series["transmit"]), seededNodeNetworkTransmit)
	})
}

// flakyOnlyPolicy grants one service, and deliberately the one the seed ranks
// *last* by CPU. A ranking that ranked the cluster and filtered the result
// afterwards would return the true top N — none of which this caller may read
// — and hand them an empty answer they could not tell from an idle cluster.
const flakyOnlyPolicy = `grants:
  - resources: ["service:shop_flaky", "node:*"]
    audience: ["group:viewers"]
    permissions: ["read"]
`

// TestMCPRankMetricsScopesByGrantBeforeRanking pins the rule rankScope exists
// for: a caller's grants have to reach the query, not the result.
//
// The distinction is invisible for a caller granted the top of the ranking and
// decisive for one granted the bottom, which is why the grant below is the
// lowest-CPU service in the seed.
func TestMCPRankMetricsScopesByGrantBeforeRanking(t *testing.T) {
	env, address, hostname := metricsCluster(t)

	policy := filepath.Join(t.TempDir(), "acl.yaml")
	if err := os.WriteFile(policy, []byte(flakyOnlyPolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	proc, _ := seedAndStart(t, env, seededMetrics(address, hostname), map[string]string{
		"CETACEAN_MCP":                  "true",
		"CETACEAN_AUTH_MODE":            "headers",
		"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
		"CETACEAN_AUTH_HEADERS_GROUPS":  "X-Auth-Groups",
		"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
		"CETACEAN_ACL_POLICY_FILE":      policy,
		// The lane's port has no host in its listen address, so no OAuth
		// issuer can be derived; the sweep's own MCP+headers SUT bypasses
		// OAuth the same way, since the identity is the proxy's header.
		"CETACEAN_MCP_AUTH_BYPASS": "headers",
	})

	viewer := readPersona{name: "viewer", user: "viewer", groups: "viewers"}

	envelope, status := mcpAs(t, proc, viewer, "tools/call", map[string]any{
		"name": "get_metrics",
		"arguments": map[string]any{
			"target": "cluster", "by": "service", "metric": "cpu", "range": "1h", "top": 3,
		},
	})

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	var result struct {
		IsError           bool `json:"isError"`
		StructuredContent struct {
			Series []struct {
				Name string `json:"name"`
			} `json:"series"`
		} `json:"structuredContent"`
	}

	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode: %v\n%s", err, envelope.Result)
	}

	if result.IsError {
		t.Fatalf("ranking failed for a caller with one grant: %s", envelope.Result)
	}

	names := make([]string, 0, len(result.StructuredContent.Series))
	for _, series := range result.StructuredContent.Series {
		names = append(names, series.Name)
	}

	if len(names) == 0 {
		t.Fatal("the ranking came back empty for a caller who may read one service, " +
			"which is what ranking the cluster and filtering afterwards produces")
	}

	if !slices.Equal(names, []string{fixtures.CrashLoopService}) {
		t.Errorf("ranked services = %v, want only %s — the rest are behind the caller's grants",
			names, fixtures.CrashLoopService)
	}
}

// ─── node pressure ──────────────────────────────────────────────────────

// atNodePressure rewrites the node's available-bytes gauges so the two
// pressure queries read the given percentages. Both are derived from the
// totals the seed already carries, so the query does the arithmetic and the
// case asserts the number a user would be shown.
func atNodePressure(series []harness.Series, diskPercent, memoryPercent float64) []harness.Series {
	out := make([]harness.Series, len(series))
	copy(out, series)

	for i := range out {
		switch out[i].Name {
		case "node_filesystem_avail_bytes":
			out[i].Start = seededDiskTotal * (1 - diskPercent/100)
		case "node_memory_MemAvailable_bytes":
			out[i].Start = seededMemoryTotal * (1 - memoryPercent/100)
		}
	}

	return out
}

// TestNodePressureRecommendationsRespectTheThreshold drives the operational
// checker's two Prometheus-dependent findings, which fire above 90% and are
// the only critical recommendations the engine can raise about a node.
//
// The boundary is asserted from both sides, which the seed makes exact: the
// rule is `usage <= 90` continues, so ninety per cent is deliberately not a
// finding and the case would fail if the comparison were loosened to `<`.
func TestNodePressureRecommendationsRespectTheThreshold(t *testing.T) {
	t.Run("at the threshold, nothing is raised", func(t *testing.T) {
		env, address, hostname := metricsCluster(t)

		proc, _ := seedAndStart(t, env,
			atNodePressure(seededMetrics(address, hostname), 90, 90), nil)

		recs := awaitRecommendations(t, proc, func(recs []recommendation) bool {
			// The sizing findings share the tick, so their arrival is the
			// signal that the operational checker has also run and said
			// nothing, rather than that it has not run yet.
			return hasCategory(recs, "no-limits")
		})

		for _, category := range []string{"node-disk-full", "node-memory-pressure"} {
			if hasCategory(recs, category) {
				t.Errorf("%s was raised at exactly 90%%, which the rule excludes", category)
			}
		}
	})

	t.Run("above it, both are raised against the node", func(t *testing.T) {
		env, address, hostname := metricsCluster(t)

		proc, _ := seedAndStart(t, env,
			atNodePressure(seededMetrics(address, hostname), 95, 93), nil)

		recs := awaitRecommendations(t, proc, func(recs []recommendation) bool {
			return hasCategory(recs, "node-disk-full") && hasCategory(recs, "node-memory-pressure")
		})

		for category, want := range map[string]string{
			"node-disk-full":       "Node disk usage is at 95%",
			"node-memory-pressure": "Node memory usage is at 93%",
		} {
			rec, ok := findCategory(recs, category)
			if !ok {
				t.Errorf("%s was not raised", category)

				continue
			}

			if rec.Severity != "critical" {
				t.Errorf("%s severity = %q, want critical", category, rec.Severity)
			}

			// Named by the hostname the cluster uses, not by the `instance`
			// label — the checker resolves one to the other through the cache,
			// and an unresolved instance would read as "10.0.0.2:9100".
			if rec.TargetName != hostname {
				t.Errorf("%s target = %q, want the node's hostname %q",
					category, rec.TargetName, hostname)
			}

			if rec.Message != want {
				t.Errorf("%s message = %q, want %q", category, rec.Message, want)
			}
		}
	})
}

// awaitRecommendations polls until `done` holds, bounded well under the
// operational checker's five-minute interval so a finding that is not here
// within it is not coming.
func awaitRecommendations(
	t *testing.T,
	proc *sut.Process,
	done func([]recommendation) bool,
) []recommendation {
	t.Helper()

	var last []recommendation

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		last = sizingRecommendations(t, proc)

		if done(last) {
			return last
		}

		time.Sleep(time.Second)
	}

	t.Fatalf("recommendations did not settle in time; last: %+v", last)

	return nil
}

func hasCategory(recs []recommendation, category string) bool {
	_, ok := findCategory(recs, category)

	return ok
}

func findCategory(recs []recommendation, category string) (recommendation, bool) {
	for _, rec := range recs {
		if rec.Category == category {
			return rec, true
		}
	}

	return recommendation{}, false
}

// ─── the SSE form of GET /metrics ───────────────────────────────────────

// TestMetricsStreamPushesTheSeededValue drives `GET /metrics` with
// `Accept: text/event-stream`, which every live chart in the dashboard opens
// after its first JSON fetch. It is a different handler from the proxy the
// same URL serves to a JSON client — content negotiation picks between them —
// and it had never been driven.
//
// The two events it emits answer different questions and are asserted apart:
// `initial` carries the whole range as a matrix, and each `point` carries one
// instant value.
func TestMetricsStreamPushesTheSeededValue(t *testing.T) {
	proc, _ := startMetricsLane(t)

	query := fmt.Sprintf(
		`sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name=%q}[5m])) * 100`,
		"shop_web",
	)

	// The smallest step the handler accepts, so a point arrives well inside
	// the read deadline below.
	path := "/metrics?" + url.Values{
		"query": {query},
		"step":  {"5"},
		"range": {"600"},
	}.Encode()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proc.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "text/event-stream")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	frames := make(chan sseFrame, 16)
	done := make(chan struct{})
	defer close(done)

	go readSSEFrames(resp.Body, frames, done)

	var sawInitial, sawPoint bool

	deadline := time.After(30 * time.Second)

	for !sawInitial || !sawPoint {
		select {
		case frame, ok := <-frames:
			if !ok {
				t.Fatal("the stream ended before both an initial and a point event arrived")
			}

			switch frame.event {
			case "initial":
				sawInitial = true

				closeTo(t, "initial matrix value",
					streamedValue(t, frame.data, "matrix"), seededServiceCPU["shop_web"])
			case "point":
				sawPoint = true

				closeTo(t, "streamed point value",
					streamedValue(t, frame.data, "vector"), seededServiceCPU["shop_web"])
			case "query_error":
				t.Fatalf("the stream reported a query error: %s", frame.data)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for events (initial: %v, point: %v)", sawInitial, sawPoint)
		}
	}
}

// streamedValue reads the last value out of a Prometheus response carried on
// the stream, asserting the result type on the way — an `initial` event is the
// whole range and a `point` is one instant, and serving one where the other
// belongs would leave a chart either empty or frozen.
func streamedValue(t *testing.T, data, wantType string) float64 {
	t.Helper()

	var body promResponse
	if err := json.Unmarshal([]byte(data), &body); err != nil {
		t.Fatalf("decode stream payload: %v\n%s", err, data)
	}

	if body.Status != "success" {
		t.Fatalf("payload status = %q, want success: %s", body.Status, data)
	}

	if body.Data.ResultType != wantType {
		t.Fatalf("resultType = %q, want %q", body.Data.ResultType, wantType)
	}

	if len(body.Data.Result) != 1 {
		t.Fatalf("got %d series, want 1: %s", len(body.Data.Result), data)
	}

	if wantType == "matrix" {
		values := body.Data.Result[0].Values
		if len(values) == 0 {
			t.Fatalf("the matrix carried no points: %s", data)
		}

		return sampleValue(t, values[len(values)-1])
	}

	return sampleValue(t, body.Data.Result[0].Value)
}

// TestMetricsStreamRefusesAMalformedRequest drives the two parameter errors
// the stream declares. Both are documented codes a client is told to expect,
// and both were reachable only through a unit test.
func TestMetricsStreamRefusesAMalformedRequest(t *testing.T) {
	proc, _ := startMetricsLane(t)

	cases := []struct {
		name  string
		query url.Values
		code  string
	}{
		{"no query", url.Values{"step": {"15"}}, "MTR003"},
		{"step below the floor", url.Values{"query": {"up"}, "step": {"1"}}, "MTR004"},
		{"step above the ceiling", url.Values{"query": {"up"}, "step": {"3600"}}, "MTR004"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(
				t.Context(), http.MethodGet, proc.BaseURL+"/metrics?"+c.query.Encode(), nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			req.Header.Set("Accept", "text/event-stream")

			resp, err := proc.Client().Do(req)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400\n%s", resp.StatusCode, body)
			}

			var problem struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(body, &problem); err != nil {
				t.Fatalf("decode problem: %v\n%s", err, body)
			}

			if want := "/api/errors/" + c.code; problem.Type != want {
				t.Errorf("type = %q, want %q", problem.Type, want)
			}
		})
	}
}
