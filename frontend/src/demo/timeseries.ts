export interface ServiceProfile {
  cpuBase: number;
  cpuSpike: number;
  memBase: number;
  memDrift: number;
  period: number;
  noiseScale: number;
}

const MB = 1024 * 1024;

export const serviceProfiles: Record<string, ServiceProfile> = {
  webshop_frontend: {
    cpuBase: 8,
    cpuSpike: 6,
    memBase: 90 * MB,
    memDrift: 15 * MB,
    period: 2,
    noiseScale: 0.08,
  },
  webshop_api: {
    cpuBase: 25,
    cpuSpike: 15,
    memBase: 320 * MB,
    memDrift: 60 * MB,
    period: 1.5,
    noiseScale: 0.12,
  },
  webshop_worker: {
    cpuBase: 12,
    cpuSpike: 20,
    memBase: 180 * MB,
    memDrift: 40 * MB,
    period: 3,
    noiseScale: 0.15,
  },
  webshop_db: {
    cpuBase: 18,
    cpuSpike: 4,
    memBase: 700 * MB,
    memDrift: 50 * MB,
    period: 6,
    noiseScale: 0.03,
  },
  webshop_cache: {
    cpuBase: 5,
    cpuSpike: 3,
    memBase: 120 * MB,
    memDrift: 30 * MB,
    period: 1,
    noiseScale: 0.1,
  },
  webshop_search: {
    cpuBase: 35,
    cpuSpike: 10,
    memBase: 1400 * MB,
    memDrift: 100 * MB,
    period: 4,
    noiseScale: 0.05,
  },
  monitoring_prometheus: {
    cpuBase: 15,
    cpuSpike: 3,
    memBase: 600 * MB,
    memDrift: 40 * MB,
    period: 8,
    noiseScale: 0.04,
  },
  monitoring_grafana: {
    cpuBase: 4,
    cpuSpike: 8,
    memBase: 150 * MB,
    memDrift: 20 * MB,
    period: 2,
    noiseScale: 0.1,
  },
  "monitoring_node-exporter": {
    cpuBase: 2,
    cpuSpike: 1,
    memBase: 30 * MB,
    memDrift: 5 * MB,
    period: 12,
    noiseScale: 0.06,
  },
  infra_proxy: {
    cpuBase: 20,
    cpuSpike: 12,
    memBase: 160 * MB,
    memDrift: 30 * MB,
    period: 1.5,
    noiseScale: 0.1,
  },
  infra_registry: {
    cpuBase: 3,
    cpuSpike: 2,
    memBase: 80 * MB,
    memDrift: 10 * MB,
    period: 6,
    noiseScale: 0.05,
  },
};

const defaultProfile: ServiceProfile = {
  cpuBase: 10,
  cpuSpike: 5,
  memBase: 200 * MB,
  memDrift: 30 * MB,
  period: 2,
  noiseScale: 0.1,
};

export function profileFor(name: string): ServiceProfile {
  return serviceProfiles[name] ?? defaultProfile;
}

/**
 * Simple seeded pseudo-random number generator (linear congruential).
 * Returns values in [0, 1).
 */
function seededRandom(seed: number): () => number {
  let state = seed | 0 || 1;
  return () => {
    state = (state * 1664525 + 1013904223) | 0;
    return (state >>> 0) / 4294967296;
  };
}

/**
 * Generate a time series of [timestamp, stringValue] pairs.
 */
export function generateTimeSeries(
  start: number,
  end: number,
  step: number,
  base: number,
  amplitude: number,
  noise: number,
  period: number,
): [number, string][] {
  const periodSeconds = period * 3600;
  const random = seededRandom(Math.round(base * 1000 + period * 7));
  const result: [number, string][] = [];

  for (let timestamp = start; timestamp <= end; timestamp += step) {
    const sinValue = Math.sin((2 * Math.PI * timestamp) / periodSeconds);
    const noiseValue = (random() - 0.5) * 2 * noise * base;
    const value = Math.max(0, base + amplitude * sinValue + noiseValue);
    result.push([timestamp, value.toFixed(4)]);
  }

  return result;
}

/**
 * Generate a single instant value with jitter.
 */
export function generateInstantValue(base: number, jitter: number): string {
  const now = Date.now() / 1000;
  const sinValue = Math.sin((2 * Math.PI * now) / 7200);
  const value = Math.max(0, base + jitter * sinValue + (Math.random() - 0.5) * 0.1 * base);
  return value.toFixed(4);
}
