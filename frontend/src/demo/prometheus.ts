import type { Dataset } from "./dataset";
import { profileFor, generateTimeSeries, generateInstantValue } from "./timeseries";

interface MetricResult {
  metric: Record<string, string>;
  value?: [number, string];
  values?: [number, string][];
}

/**
 * Handle an instant Prometheus query, returning vector results.
 */
export function handleInstantQuery(
  query: string,
  dataset: Dataset,
): { resultType: "vector" | "matrix" | "scalar" | "string"; result: MetricResult[] } {
  const now = Date.now() / 1000;
  const result = matchQuery(query, dataset, (profile, labels) => ({
    metric: labels,
    value: [now, generateInstantValue(profile.base, profile.amplitude)] as [number, string],
  }));

  return { resultType: "vector", result };
}

/**
 * Handle a range Prometheus query, returning matrix results.
 */
export function handleRangeQuery(
  query: string,
  start: number,
  end: number,
  step: number,
  dataset: Dataset,
): { resultType: "vector" | "matrix" | "scalar" | "string"; result: MetricResult[] } {
  const result = matchQuery(query, dataset, (profile, labels) => ({
    metric: labels,
    values: generateTimeSeries(
      start,
      end,
      step,
      profile.base,
      profile.amplitude,
      profile.noise,
      profile.period,
    ),
  }));

  return { resultType: "matrix", result };
}

interface ProfileParams {
  base: number;
  amplitude: number;
  noise: number;
  period: number;
}

/**
 * Pattern-match on the query string and produce results using the callback.
 */
function matchQuery(
  query: string,
  dataset: Dataset,
  build: (profile: ProfileParams, labels: Record<string, string>) => MetricResult,
): MetricResult[] {
  if (query.includes("up{")) {
    return dataset.nodes.map((node) =>
      build(
        { base: 1, amplitude: 0, noise: 0, period: 1 },
        {
          __name__: "up",
          job: "node-exporter",
          instance: `${node.Description.Hostname}:9100`,
          nodename: node.Description.Hostname,
        },
      ),
    );
  }

  if (query.includes("container_cpu")) {
    return dataset.services.map((service) => {
      const profile = profileFor(service.Spec.Name);
      return build(
        {
          base: profile.cpuBase,
          amplitude: profile.cpuSpike,
          noise: profile.noiseScale,
          period: profile.period,
        },
        {
          __name__: "container_cpu_usage_seconds_total",
          name: service.Spec.Name,
          container_label_com_docker_swarm_service_name: service.Spec.Name,
        },
      );
    });
  }

  if (query.includes("container_memory")) {
    return dataset.services.map((service) => {
      const profile = profileFor(service.Spec.Name);
      return build(
        {
          base: profile.memBase,
          amplitude: profile.memDrift,
          noise: profile.noiseScale,
          period: profile.period,
        },
        {
          __name__: "container_memory_usage_bytes",
          name: service.Spec.Name,
          container_label_com_docker_swarm_service_name: service.Spec.Name,
        },
      );
    });
  }

  if (query.includes("node_filesystem")) {
    return dataset.nodes.map((node, index) => {
      const basePercent = 35 + index * 5;
      return build(
        { base: basePercent, amplitude: 3, noise: 0.02, period: 24 },
        {
          __name__: "node_filesystem_avail_bytes",
          instance: `${node.Description.Hostname}:9100`,
          nodename: node.Description.Hostname,
          mountpoint: "/",
          device: "/dev/sda1",
        },
      );
    });
  }

  if (query.includes("node_memory")) {
    return dataset.nodes.map((node, index) => {
      const basePercent = 55 + index * 8;
      return build(
        { base: basePercent, amplitude: 5, noise: 0.04, period: 6 },
        {
          __name__: "node_memory_MemAvailable_bytes",
          instance: `${node.Description.Hostname}:9100`,
          nodename: node.Description.Hostname,
        },
      );
    });
  }

  if (query.includes("changes(container_last_seen")) {
    return dataset.services.map((service) => {
      const base = service.Spec.Name === "webshop_api" ? 3 : 0;
      return build(
        { base, amplitude: 0, noise: 0, period: 1 },
        {
          name: service.Spec.Name,
          container_label_com_docker_swarm_service_name: service.Spec.Name,
        },
      );
    });
  }

  // Default: per-service using cpuBase
  return dataset.services.map((service) => {
    const profile = profileFor(service.Spec.Name);
    return build(
      {
        base: profile.cpuBase,
        amplitude: profile.cpuSpike,
        noise: profile.noiseScale,
        period: profile.period,
      },
      {
        name: service.Spec.Name,
        container_label_com_docker_swarm_service_name: service.Spec.Name,
      },
    );
  });
}
