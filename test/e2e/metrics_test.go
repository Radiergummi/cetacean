//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
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

var seededServiceMemory = map[string]float64{
	"shop_web":                512 * 1024 * 1024,
	"shop_lonely":             128 * 1024 * 1024,
	fixtures.CrashLoopService: 64 * 1024 * 1024,
	"platform_agent":          256 * 1024 * 1024,
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
