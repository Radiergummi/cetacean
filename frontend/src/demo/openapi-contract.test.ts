/// <reference types="node" />
/**
 * Validates every demo MSW handler's response against the OpenAPI spec.
 * Catches drift between the demo mock and the documented response shape.
 *
 * The endpoint list is shared with `handlers.test.ts` via `endpoints.ts` so
 * the two tests cannot silently diverge.
 */
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { load as parseYAML } from "js-yaml";
import { setupServer } from "msw/node";
import OpenAPIResponseValidator from "openapi-response-validator";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";

import { buildDataset } from "./dataset";
import { buildDemoEndpoints, shouldSkipContract } from "./endpoints";
import { createHandlers } from "./handlers";
import type { SSEClients } from "./sseHandlers";

const dataset = buildDataset();
const clients: SSEClients = { global: new Set(), byType: new Map(), byId: new Map() };
const handlers = createHandlers(dataset, clients);
const server = setupServer(...handlers);

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterAll(() => server.close());

interface OpenAPIOperation {
  responses: Record<string, {
    content?: Record<string, { schema?: unknown }>;
  }>;
}

interface OpenAPISpec {
  paths: Record<string, Record<string, OpenAPIOperation>>;
  components?: { schemas?: Record<string, unknown> };
}

const moduleDir = dirname(fileURLToPath(import.meta.url));
const specPath = resolve(moduleDir, "../../../api/openapi.yaml");
const spec = parseYAML(readFileSync(specPath, "utf-8")) as OpenAPISpec;

// Precompile a path-template → regex map once at module load so findOperation
// doesn't rebuild ~60 regexes on every test case.
const templateRegexes = new Map<string, RegExp>();
for (const template of Object.keys(spec.paths)) {
  templateRegexes.set(
    template,
    new RegExp("^" + template.replace(/\{[^}]+\}/g, "[^/]+") + "$"),
  );
}

// Cache validators by path template so they're built at most once per template
// per test run (openapi-response-validator builds JSON Schema validators
// eagerly at construction time; that work is amortised across 50+ tests).
const validatorCache = new Map<string, OpenAPIResponseValidator>();

function getValidator(
  pathTemplate: string,
  operation: OpenAPIOperation,
): OpenAPIResponseValidator {
  let v = validatorCache.get(pathTemplate);
  if (!v) {
    // Cast: the library's types are stricter (expect fully-resolved SchemaObject)
    // than our loose YAML parsing produces, but the validator handles $refs at runtime.
    v = new OpenAPIResponseValidator({
      responses: operation.responses as never,
      components: (spec.components ?? { schemas: {} }) as never,
    });
    validatorCache.set(pathTemplate, v);
  }
  return v;
}

/**
 * Endpoints where the demo response shape doesn't match the spec. These are
 * places where the demo mock returns null for empty arrays instead of [].
 *
 * Not every entry in the Go `knownDriftEndpoints` map (see
 * internal/api/openapi_exhaustive_test.go) appears here: the demo mocks are
 * hand-authored, so some of the nullability issues that the Go handlers
 * exhibit don't occur in the demo. Treat the two lists as independent.
 */
const knownDriftEndpoints = new Set<string>([
  "/services/{id}/configs",
  "/services/{id}/secrets",
  "/services/{id}/networks",
  "/services/{id}/mounts",
]);

function findOperation(
  method: string,
  path: string,
): { pathTemplate: string; operation: OpenAPIOperation } | null {
  const pathOnly = path.split("?")[0];
  const methodLower = method.toLowerCase();

  const direct = spec.paths[pathOnly]?.[methodLower];
  if (direct) {
    return { pathTemplate: pathOnly, operation: direct };
  }

  for (const [template, methods] of Object.entries(spec.paths)) {
    const op = methods[methodLower];
    if (!op) {
      continue;
    }
    const regex = templateRegexes.get(template);
    if (regex && regex.test(pathOnly)) {
      return { pathTemplate: template, operation: op };
    }
  }

  return null;
}

describe("demo handler responses match OpenAPI spec", () => {
  const endpoints = buildDemoEndpoints(dataset).filter((p) => !shouldSkipContract(p));

  it.each(endpoints)("GET %s matches spec", async (path) => {
    const match = findOperation("get", path);
    expect(match, `no spec operation for GET ${path}`).not.toBeNull();
    if (!match) {
      return;
    }

    if (knownDriftEndpoints.has(match.pathTemplate)) {
      return;
    }

    const response = await fetch(`http://localhost${path}`, {
      headers: { Accept: "application/json" },
    });

    if (!response.ok) {
      return;
    }

    const body = await response.json().catch(() => null);
    if (body === null) {
      return;
    }

    const validator = getValidator(match.pathTemplate, match.operation);
    const errors = validator.validateResponse(response.status, body);

    expect(
      errors,
      `response body does not match spec for ${path} (${match.pathTemplate}):\n${JSON.stringify(errors, null, 2)}`,
    ).toBeFalsy();
  });
});
