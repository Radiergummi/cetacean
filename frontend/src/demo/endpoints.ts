/**
 * Canonical list of GET endpoints served by the demo. Shared between
 * `handlers.test.ts` (coverage) and `openapi-contract.test.ts` (spec
 * conformance) so the two tests cannot silently diverge.
 *
 * When you add a new demo handler in `handlers.ts`, add its path here.
 */
import type { Dataset } from "./dataset";

export function buildDemoEndpoints(dataset: Dataset): string[] {
  // The demo dataset is always fully populated; the fallbacks only exist so
  // the list stays well-typed under noUncheckedIndexedAccess.
  const nodeId = dataset.nodes[0]?.ID ?? "";
  const serviceId = dataset.services[0]?.ID ?? "";
  const taskId = dataset.tasks[0]?.ID ?? "";
  const configId = dataset.configs[0]?.ID ?? "";
  const secretId = dataset.secrets[0]?.ID ?? "";
  const networkId = dataset.networks[0]?.Id ?? "";
  const volumeName = dataset.volumes[0]?.Name ?? "";

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
    `/nodes/${nodeId}`,
    `/nodes/${nodeId}/tasks`,
    `/nodes/${nodeId}/labels`,
    `/nodes/${nodeId}/role`,
    `/services/${serviceId}`,
    `/services/${serviceId}/tasks`,
    `/services/${serviceId}/env`,
    `/services/${serviceId}/labels`,
    `/services/${serviceId}/resources`,
    `/services/${serviceId}/healthcheck`,
    `/services/${serviceId}/configs`,
    `/services/${serviceId}/secrets`,
    `/services/${serviceId}/networks`,
    `/services/${serviceId}/mounts`,
    `/services/${serviceId}/ports`,
    `/services/${serviceId}/placement`,
    `/services/${serviceId}/update-policy`,
    `/services/${serviceId}/rollback-policy`,
    `/services/${serviceId}/log-driver`,
    `/services/${serviceId}/container-config`,
    `/services/${serviceId}/logs`,
    `/tasks/${taskId}`,
    `/tasks/${taskId}/logs`,
    `/stacks/webshop`,
    `/configs/${configId}`,
    `/secrets/${secretId}`,
    `/networks/${networkId}`,
    `/volumes/${volumeName}`,

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
    "/topology",
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
  const pathOnly = path.split("?")[0] ?? path;

  if (nonContractEndpoints.has(pathOnly)) {
    return true;
  }

  if (pathOnly.endsWith("/logs")) {
    return true;
  }

  return false;
}
