//go:build e2e

package fixtures

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/test/e2e/harness"
)

// NodeIdentityCLI reports the node's address and hostname. The address is the
// half of node-exporter's `instance` label Cetacean matches on, and Docker
// assigns it, so no seed can be written down ahead of the cluster.
func NodeIdentityCLI(env *harness.Env) (address, hostname string, err error) {
	nodes, err := env.Docker.NodeList(context.Background(), swarm.NodeListOptions{})
	if err != nil {
		return "", "", fmt.Errorf("NodeList: %w", err)
	}

	if len(nodes) != 1 {
		return "", "", fmt.Errorf("expected one node, got %d", len(nodes))
	}

	if nodes[0].Status.Addr == "" {
		return "", "", errors.New("the node reports no address")
	}

	return nodes[0].Status.Addr, nodes[0].Description.Hostname, nil
}

// This file describes the fixture cluster's metrics the way fixtures.go
// describes its resources: what a Prometheus scraping this cluster would hold.
// The Go lane and `make e2e-up` seed the same numbers, so the browser suite and
// metrics_test.go look at one cluster.

// The seeded utilisation. Distinct on purpose: were they equal, a query
// reading the wrong metric family would still produce the expected number.
const (
	SeededNodeCPUPercent    = 25.0
	SeededNodeMemoryPercent = 40.0
	SeededNodeDiskPercent   = 60.0

	SeededMemoryTotal = 10 * 1024 * 1024 * 1024 // 10 GiB
	SeededMemoryAvail = 6 * 1024 * 1024 * 1024  // 6 GiB, so 4 GiB used
	SeededDiskTotal   = 100 * 1024 * 1024 * 1024
	SeededDiskAvail   = 40 * 1024 * 1024 * 1024 // 60 GiB used
)

// Per-service CPU, as the percentage `sum(rate(...)) * 100` yields, and
// per-service memory in bytes.
var SeededServiceCPU = map[string]float64{
	"shop_web":       50,
	"shop_lonely":    10,
	CrashLoopService: 2,
	"platform_agent": 30,
}

// Memory deliberately ranks the services in the exact reverse of CPU. A
// ranking that read the wrong metric would otherwise still come back in the
// right order, and the cases below assert an order.
var SeededServiceMemory = map[string]float64{
	CrashLoopService: 512 * 1024 * 1024,
	"shop_lonely":    256 * 1024 * 1024,
	"platform_agent": 128 * 1024 * 1024,
	"shop_web":       64 * 1024 * 1024,
}

// Which stack each service belongs to, for the per-stack rollup.
var SeededServiceStack = map[string]string{
	"shop_web":       StackShop,
	"shop_lonely":    StackShop,
	CrashLoopService: StackShop,
	"platform_agent": StackPlatform,
}

const (
	SeededNetworkReceive  = 1000.0 // bytes/second, shop_web
	SeededNetworkTransmit = 500.0

	SeededNodeNetworkReceive  = 2000.0 // bytes/second, on eth0
	SeededNodeNetworkTransmit = 750.0
)

// ─── the seed ───────────────────────────────────────────────────────────

// MetricsSeed builds the exposition for a node reachable at `address`. The
// address is the half of node-exporter's `instance` label Cetacean matches on
// (internal/mcp/metrics.go's instanceSelector), and it is assigned by Docker,
// so the series cannot be written down ahead of the cluster.
func MetricsSeed(address, hostname string) []harness.Series {
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
		counter("node_cpu_seconds_total", withMetricLabels(nodeLabels, map[string]string{
			"cpu": "0", "mode": "idle",
		}), 1-SeededNodeCPUPercent/100),
		counter("node_cpu_seconds_total", withMetricLabels(nodeLabels, map[string]string{
			"cpu": "0", "mode": "user",
		}), SeededNodeCPUPercent/100),

		gauge("node_memory_MemTotal_bytes", nodeLabels, SeededMemoryTotal),
		gauge("node_memory_MemAvailable_bytes", nodeLabels, SeededMemoryAvail),

		gauge("node_filesystem_size_bytes", withMetricLabels(nodeLabels, map[string]string{
			"mountpoint": "/", "fstype": "ext4",
		}), SeededDiskTotal),
		gauge("node_filesystem_avail_bytes", withMetricLabels(nodeLabels, map[string]string{
			"mountpoint": "/", "fstype": "ext4",
		}), SeededDiskAvail),

		counter("node_network_receive_bytes_total", withMetricLabels(nodeLabels, map[string]string{
			"device": "eth0",
		}), SeededNodeNetworkReceive),
		counter("node_network_transmit_bytes_total", withMetricLabels(nodeLabels, map[string]string{
			"device": "eth0",
		}), SeededNodeNetworkTransmit),
		// Loopback, which every node query excludes with device!="lo". Seeded
		// large enough that a query forgetting the exclusion reads wrong.
		counter("node_network_receive_bytes_total", withMetricLabels(nodeLabels, map[string]string{
			"device": "lo",
		}), 9_000_000),
	}

	for name, cpu := range SeededServiceCPU {
		labels := map[string]string{
			"instance": cadvisorInstance,
			"job":      "cadvisor",
			// The three labels every container query in the product selects
			// on. The id is only ever tested for emptiness, so it need not be
			// the service's real one.
			"container_label_com_docker_swarm_service_name": name,
			"container_label_com_docker_swarm_service_id":   "seeded-" + name,
			"container_label_com_docker_stack_namespace":    SeededServiceStack[name],
		}

		series = append(series,
			counter("container_cpu_usage_seconds_total", labels, cpu/100),
			gauge("container_memory_usage_bytes", labels, SeededServiceMemory[name]),
		)

		if name == "shop_web" {
			series = append(series,
				counter("container_network_receive_bytes_total", labels, SeededNetworkReceive),
				counter("container_network_transmit_bytes_total", labels, SeededNetworkTransmit),
			)
		}
	}

	return series
}

func withMetricLabels(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	maps.Copy(out, base)
	maps.Copy(out, extra)

	return out
}

// ServiceSeries is the cAdvisor half of the seed for one service: the three
// labels every container query in the product selects on, a CPU counter rising
// at the given percentage of a core, and a flat memory gauge.
func ServiceSeries(
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
