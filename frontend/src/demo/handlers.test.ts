/**
 * Verifies that every API client method has a matching MSW handler.
 *
 * Calls each api.* method against the demo handlers and asserts none
 * return a network error (which would mean MSW didn't intercept it).
 * This catches new endpoints added to client.ts without a corresponding
 * demo handler.
 */
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { setupServer } from "msw/node";
import { buildDataset } from "./dataset";
import { createHandlers } from "./handlers";
import type { SSEClients } from "./sseHandlers";

const dataset = buildDataset();
const clients: SSEClients = { global: new Set(), byType: new Map(), byId: new Map() };
const handlers = createHandlers(dataset, clients);

const server = setupServer(...handlers);

beforeAll(() =>
  server.listen({
    onUnhandledRequest: "error",
  }),
);
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

/**
 * All GET endpoints from the API client that should have demo handlers.
 * When you add a new endpoint to client.ts, add it here too — the test
 * will fail until you add a matching MSW handler.
 */
const getEndpoints = [
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

  // Resources (lists)
  "/nodes",
  "/services",
  "/tasks",
  "/stacks",
  "/stacks/summary",
  "/configs",
  "/secrets",
  "/networks",
  "/volumes",

  // Resource details (use first item from dataset)
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

describe("demo handler coverage", () => {
  it.each(getEndpoints)("GET %s returns a response", async (path) => {
    const response = await fetch(`http://localhost${path}`, {
      headers: { Accept: "application/json", Range: "items 0-49" },
    });

    expect(response.status, `${path} returned ${response.status}`).toBeLessThan(500);
  });
});
