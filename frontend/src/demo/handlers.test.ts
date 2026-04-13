/**
 * Verifies that every API endpoint has a matching MSW demo handler and
 * that response shapes match what the frontend expects. Adding a new
 * endpoint to client.ts without a corresponding handler here will fail
 * the coverage test.
 */
import { buildDataset } from "./dataset";
import { createHandlers } from "./handlers";
import type { SSEClients } from "./sseHandlers";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";

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

    expect(response.status).toBeLessThan(500);
  });
});

async function fetchJSON(path: string) {
  const response = await fetch(`http://localhost${path}`, {
    headers: { Accept: "application/json", Range: "items 0-49" },
  });
  if (!response.ok) {
    throw new Error(`${path} returned ${response.status}`);
  }
  return response.json();
}

describe("collection response shapes", () => {
  it("GET /nodes returns 3 nodes", async () => {
    const data = await fetchJSON("/nodes");
    expect(Array.isArray(data.items)).toBe(true);
    expect(data.total).toBe(3);
  });

  it("GET /services returns 11 services with RunningTasks", async () => {
    const data = await fetchJSON("/services");
    expect(data.total).toBe(11);
    for (const item of data.items) {
      expect(item).toHaveProperty("RunningTasks");
    }
  });

  it("GET /tasks returns items array", async () => {
    const data = await fetchJSON("/tasks");
    expect(Array.isArray(data.items)).toBe(true);
    expect(typeof data.total).toBe("number");
  });

  it("GET /stacks returns items array", async () => {
    const data = await fetchJSON("/stacks");
    expect(Array.isArray(data.items)).toBe(true);
    expect(typeof data.total).toBe("number");
  });

  it("GET /configs returns 3 configs", async () => {
    const data = await fetchJSON("/configs");
    expect(data.total).toBe(3);
  });

  it("GET /secrets returns 2 secrets", async () => {
    const data = await fetchJSON("/secrets");
    expect(data.total).toBe(2);
  });

  it("GET /networks returns 5 networks", async () => {
    const data = await fetchJSON("/networks");
    expect(data.total).toBe(5);
  });

  it("GET /volumes returns 2 volumes", async () => {
    const data = await fetchJSON("/volumes");
    expect(data.total).toBe(2);
  });
});

describe("detail response shapes", () => {
  it("GET /nodes/:id has node with ID and Hostname", async () => {
    const data = await fetchJSON(`/nodes/${dataset.nodes[0].ID}`);
    expect(data).toHaveProperty("node");
    expect(data.node).toHaveProperty("ID");
    expect(data.node.Description).toHaveProperty("Hostname");
  });

  it("GET /services/:id has service, changes, integrations", async () => {
    const data = await fetchJSON(`/services/${dataset.services[0].ID}`);
    expect(data).toHaveProperty("service");
    expect(Array.isArray(data.changes)).toBe(true);
    expect(Array.isArray(data.integrations)).toBe(true);
  });

  it("GET /configs/:id has config and services", async () => {
    const data = await fetchJSON(`/configs/${dataset.configs[0].ID}`);
    expect(data).toHaveProperty("config");
    expect(Array.isArray(data.services)).toBe(true);
  });

  it("GET /secrets/:id has secret and services", async () => {
    const data = await fetchJSON(`/secrets/${dataset.secrets[0].ID}`);
    expect(data).toHaveProperty("secret");
    expect(Array.isArray(data.services)).toBe(true);
  });

  it("GET /networks/:id has network and services", async () => {
    const data = await fetchJSON(`/networks/${dataset.networks[0].Id}`);
    expect(data).toHaveProperty("network");
    expect(Array.isArray(data.services)).toBe(true);
  });

  it("GET /volumes/:name has volume and services", async () => {
    const data = await fetchJSON(`/volumes/${dataset.volumes[0].Name}`);
    expect(data).toHaveProperty("volume");
    expect(Array.isArray(data.services)).toBe(true);
  });

  it("GET /stacks/webshop has stack with sub-resource arrays", async () => {
    const data = await fetchJSON("/stacks/webshop");
    expect(data).toHaveProperty("stack");
    expect(Array.isArray(data.stack.services)).toBe(true);
    expect(Array.isArray(data.stack.configs)).toBe(true);
    expect(Array.isArray(data.stack.secrets)).toBe(true);
    expect(Array.isArray(data.stack.networks)).toBe(true);
    expect(Array.isArray(data.stack.volumes)).toBe(true);
  });
});

describe("computed endpoint shapes", () => {
  it("GET /cluster has count fields", async () => {
    const data = await fetchJSON("/cluster");
    expect(typeof data.nodeCount).toBe("number");
    expect(typeof data.serviceCount).toBe("number");
    expect(typeof data.taskCount).toBe("number");
    expect(typeof data.stackCount).toBe("number");
  });

  it("GET /cluster/metrics has cpu, memory, disk", async () => {
    const data = await fetchJSON("/cluster/metrics");
    for (const key of ["cpu", "memory", "disk"] as const) {
      expect(data[key]).toHaveProperty("used");
      expect(data[key]).toHaveProperty("total");
      expect(data[key]).toHaveProperty("percent");
    }
  });

  it("GET /stacks/summary has items with expected fields", async () => {
    const data = await fetchJSON("/stacks/summary");
    expect(Array.isArray(data.items)).toBe(true);
    for (const item of data.items) {
      expect(typeof item.name).toBe("string");
      expect(typeof item.serviceCount).toBe("number");
      expect(typeof item.desiredTasks).toBe("number");
      expect(item).toHaveProperty("tasksByState");
    }
  });

  it("GET /search?q=webshop has query, results, counts, total", async () => {
    const data = await fetchJSON("/search?q=webshop");
    expect(typeof data.query).toBe("string");
    expect(data).toHaveProperty("results");
    expect(data).toHaveProperty("counts");
    expect(typeof data.total).toBe("number");
  });

  it("GET /recommendations has items, total, summary", async () => {
    const data = await fetchJSON("/recommendations");
    expect(Array.isArray(data.items)).toBe(true);
    expect(typeof data.total).toBe("number");
    expect(data).toHaveProperty("summary");
  });

  it("GET /-/health has status, version, operationsLevel", async () => {
    const data = await fetchJSON("/-/health");
    expect(typeof data.status).toBe("string");
    expect(typeof data.version).toBe("string");
    expect(typeof data.operationsLevel).toBe("number");
  });

  it("GET /metrics/status has prometheusConfigured, nodeExporter, cadvisor", async () => {
    const data = await fetchJSON("/metrics/status");
    expect(typeof data.prometheusConfigured).toBe("boolean");
    expect(data).toHaveProperty("nodeExporter");
    expect(data).toHaveProperty("cadvisor");
  });
});

describe("topology endpoint shapes", () => {
  it("GET /topology/networks has nodes, edges, networks", async () => {
    const data = await fetchJSON("/topology/networks");
    expect(data["@context"]).toBeTruthy();
    expect(data["@id"]).toBeTruthy();
    expect(data["@type"]).toBe("NetworkTopology");
    expect(Array.isArray(data.nodes)).toBe(true);
    expect(Array.isArray(data.edges)).toBe(true);
    expect(Array.isArray(data.networks)).toBe(true);
  });

  it("GET /topology/placement has nodes with tasks", async () => {
    const data = await fetchJSON("/topology/placement");
    expect(data["@context"]).toBeTruthy();
    expect(data["@id"]).toBeTruthy();
    expect(data["@type"]).toBe("PlacementTopology");
    expect(Array.isArray(data.nodes)).toBe(true);

    for (const node of data.nodes) {
      expect(Array.isArray(node.tasks)).toBe(true);
    }
  });
});

describe("metrics endpoint shapes", () => {
  it("GET /metrics?query=up returns instant vector", async () => {
    const data = await fetchJSON("/metrics?query=up");
    expect(data.data).toHaveProperty("resultType");
    expect(data.data.resultType).toBe("vector");
    expect(Array.isArray(data.data.result)).toBe(true);
  });

  it("GET /metrics range query returns matrix", async () => {
    const data = await fetchJSON("/metrics?query=cpu&start=0&end=100&step=15");
    expect(data.data).toHaveProperty("resultType");
    expect(data.data.resultType).toBe("matrix");
    expect(Array.isArray(data.data.result)).toBe(true);
  });
});

describe("write operations", () => {
  it("PUT /services/:id/scale mutates replicas", async () => {
    const service = dataset.services.find((service) => service.Spec.Mode.Replicated !== undefined)!;
    const response = await fetch(`http://localhost/services/${service.ID}/scale`, {
      method: "PUT",
      headers: { Accept: "application/json", "Content-Type": "application/json" },
      body: JSON.stringify({ replicas: 5 }),
    });
    expect(response.ok).toBe(true);
    const data = await response.json();
    expect(data.Spec.Mode.Replicated.Replicas).toBe(5);
  });

  it("DELETE /tasks/:id returns 204", async () => {
    const task = dataset.tasks.find((task) => task.Status.State === "running")!;
    const response = await fetch(`http://localhost/tasks/${task.ID}`, {
      method: "DELETE",
      headers: { Accept: "application/json" },
    });
    expect(response.status).toBe(204);
  });
});
