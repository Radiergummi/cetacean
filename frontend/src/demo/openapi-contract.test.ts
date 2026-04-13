/**
 * Validates every demo MSW handler's response against the OpenAPI spec.
 * Catches drift between the demo mock and the documented response shape.
 *
 * The `getEndpoints` array from handlers.test.ts is the source of truth for
 * which demo routes exist; this test iterates that list, hits each one via
 * MSW, and validates the response body against the matching spec operation.
 *
 * Endpoints listed in `knownDriftEndpoints` are skipped — they document
 * outstanding mismatches between the demo and the spec. Each entry should be
 * removed once the demo or spec is aligned.
 */
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { load as parseYAML } from "js-yaml";
import { setupServer } from "msw/node";
import OpenAPIResponseValidator from "openapi-response-validator";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";

import { buildDataset } from "./dataset";
import { createHandlers } from "./handlers";
import type { SSEClients } from "./sseHandlers";

const dataset = buildDataset();
const clients: SSEClients = { global: new Set(), byType: new Map(), byId: new Map() };
const handlers = createHandlers(dataset, clients);
const server = setupServer(...handlers);

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

interface OpenAPISpec {
  paths: Record<string, Record<string, {
    responses: Record<string, {
      content?: Record<string, { schema?: unknown }>;
    }>;
  }>>;
  components?: { schemas?: Record<string, unknown> };
}

const specPath = resolve(__dirname, "../../../api/openapi.yaml");
const spec = parseYAML(readFileSync(specPath, "utf-8")) as OpenAPISpec;

/**
 * Endpoints where the demo response shape currently doesn't match the spec.
 * Remove entries as demo handlers are updated.
 */
const knownDriftEndpoints = new Set<string>([
  // Demo returns null for empty arrays; spec expects []. Matches the Go
  // handler drift tracked in openapi_exhaustive_test.go.
  "/services/{id}/configs",
  "/services/{id}/secrets",
  "/services/{id}/networks",
  "/services/{id}/mounts",
]);

function findOperation(method: string, path: string): { pathTemplate: string; operation: unknown } | null {
  // Strip query string.
  const pathOnly = path.split("?")[0];
  const methodLower = method.toLowerCase();

  // Direct match first.
  const direct = spec.paths[pathOnly]?.[methodLower];
  if (direct) {
    return { pathTemplate: pathOnly, operation: direct };
  }

  // Try template match: spec uses {id}, actual path has real value.
  for (const [template, methods] of Object.entries(spec.paths)) {
    if (!methods[methodLower]) {
      continue;
    }
    const regex = new RegExp("^" + template.replace(/\{[^}]+\}/g, "[^/]+") + "$");
    if (regex.test(pathOnly)) {
      return { pathTemplate: template, operation: methods[methodLower] };
    }
  }

  return null;
}

function endpointsFromHandlersTest(): string[] {
  // Mirrors getEndpoints from handlers.test.ts. Kept here as a copy to keep
  // the two tests independent.
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

    // Lists
    "/nodes",
    "/services",
    "/tasks",
    "/stacks",
    "/stacks/summary",
    "/configs",
    "/secrets",
    "/networks",
    "/volumes",
    "/history",
    "/recommendations",
    "/disk-usage",
    "/plugins",

    // Resource details
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
    `/tasks/${dataset.tasks[0].ID}`,
    `/stacks/webshop`,
    `/configs/${dataset.configs[0].ID}`,
    `/secrets/${dataset.secrets[0].ID}`,
    `/networks/${dataset.networks[0].Id}`,
    `/volumes/${dataset.volumes[0].Name}`,

    // Topology
    "/topology/networks",
    "/topology/placement",
  ];
}

describe("demo handler responses match OpenAPI spec", () => {
  const endpoints = endpointsFromHandlersTest();

  it.each(endpoints)("GET %s matches spec", async (path) => {
    const match = findOperation("get", path);
    if (!match) {
      // No spec operation for this path — handlers.test.ts already asserts
      // 200, so a missing spec is a separate issue (or the endpoint is
      // intentionally undocumented).
      return;
    }

    if (knownDriftEndpoints.has(match.pathTemplate)) {
      return;
    }

    const response = await fetch(`http://localhost${path}`, {
      headers: { Accept: "application/json" },
    });

    // Skip non-2xx — these may legitimately not match a 200 schema.
    if (!response.ok) {
      return;
    }

    const body = await response.json().catch(() => null);
    if (body === null) {
      return;
    }

    const validator = new OpenAPIResponseValidator({
      // @ts-expect-error — library types don't exactly match our shape but runtime works
      responses: match.operation.responses,
      components: spec.components ?? { schemas: {} },
    });

    const errors = validator.validateResponse(response.status, body);

    expect(errors, `response body does not match spec for ${path} (${match.pathTemplate}):\n${JSON.stringify(errors, null, 2)}`).toBeFalsy();
  });
});
