/**
 * Canonical list of GET endpoints served by the demo. Shared between
 * `handlers.test.ts` (coverage) and `openapi-contract.test.ts` (spec
 * conformance) so the two tests cannot silently diverge.
 *
 * When you add a new demo handler in `handlers.ts`, add its path here.
 */
import type { Dataset } from "./dataset";

export function buildDemoEndpoints(dataset: Dataset): string[] {
  return [
    // Meta
    "/-/health",
    "/-/ready",
    "/-/docker-latest-version",
    "/profile",
    "/auth/whoami",

    // Cluster
    "/cluster",
    "/cluster/metrics",
    "/cluster/capacity",
    "/swarm",

    // Resource lists
    "/nodes",
    "/services",
    "/tasks",
    "/stacks",
    "/stacks/summary",
    "/configs",
    "/secrets",
    "/networks",
    "/volumes",

    // Resource details (first item from dataset)
    `/nodes/${dataset.nodes[0].ID}`,
    `/nodes/${dataset.nodes[0].ID}/tasks`,
    `/nodes/${dataset.nodes[0].ID}/labels`,
    `/nodes/${dataset.nodes[0].ID}/role`,
    `/services/${dataset.services[0].ID}`,
    `/services/${dataset.services[0].ID}/tasks`,
    `/services/${dataset.services[0].ID}/env`,
    `/services/${dataset.services[0].ID}/labels`,
    `/services/${dataset.services[0].ID}/resources`,
    `/services/${dataset.services[0].ID}/healthcheck`,
    `/services/${dataset.services[0].ID}/configs`,
    `/services/${dataset.services[0].ID}/secrets`,
    `/services/${dataset.services[0].ID}/networks`,
    `/services/${dataset.services[0].ID}/mounts`,
    `/services/${dataset.services[0].ID}/ports`,
    `/services/${dataset.services[0].ID}/placement`,
    `/services/${dataset.services[0].ID}/update-policy`,
    `/services/${dataset.services[0].ID}/rollback-policy`,
    `/services/${dataset.services[0].ID}/log-driver`,
    `/services/${dataset.services[0].ID}/container-config`,
    `/services/${dataset.services[0].ID}/logs`,
    `/tasks/${dataset.tasks[0].ID}`,
    `/tasks/${dataset.tasks[0].ID}/logs`,
    `/stacks/webshop`,
    `/configs/${dataset.configs[0].ID}`,
    `/secrets/${dataset.secrets[0].ID}`,
    `/networks/${dataset.networks[0].Id}`,
    `/volumes/${dataset.volumes[0].Name}`,

    // Search
    "/search?q=web",

    // Metrics
    "/metrics/status",
    "/metrics/labels",
    "/metrics/labels/job",
    "/metrics?query=up",

    // Other
    "/history",
    "/recommendations",
    "/disk-usage",
    "/plugins",
    "/topology/networks",
    "/topology/placement",
  ];
}

/**
 * Endpoints that the contract test skips because their responses are either
 * streaming (SSE), proxied passthroughs, or intentionally not JSON.
 * Keys are path-only (no query string) — shouldSkipContract strips queries
 * before checking.
 */
export const nonContractEndpoints = new Set<string>([
  "/metrics",
  "/metrics/labels",
  "/metrics/labels/job",
]);

/**
 * Returns true if `path` matches a contract-skip pattern. Patterns like
 * `/services/{id}/logs` and `/tasks/{id}/logs` are matched by suffix.
 */
export function shouldSkipContract(path: string): boolean {
  const pathOnly = path.split("?")[0];

  if (nonContractEndpoints.has(pathOnly)) {
    return true;
  }

  if (pathOnly.endsWith("/logs")) {
    return true;
  }

  return false;
}
