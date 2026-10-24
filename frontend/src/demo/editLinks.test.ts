/// <reference types="node" />
/**
 * Holds the demo's `rel="edit"` table against api/openapi.yaml: a detail
 * resource links each sub-resource directly beneath it that takes a write.
 */
import { editableByPath } from "./handlers";
import { load as parseYAML } from "js-yaml";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const writeMethods = ["put", "post", "patch", "delete"];

const moduleDir = dirname(fileURLToPath(import.meta.url));
const spec = parseYAML(readFileSync(resolve(moduleDir, "../../../api/openapi.yaml"), "utf-8")) as {
  paths: Record<string, Record<string, unknown>>;
};

/** The sub-resources each path template has a write route directly beneath. */
const writableBelow = new Map<string, string[]>();
for (const [template, operations] of Object.entries(spec.paths)) {
  if (!writeMethods.some((method) => method in operations)) {
    continue;
  }

  const cut = template.lastIndexOf("/");
  const [parent, name] = [template.slice(0, cut), template.slice(cut + 1)];
  if (!name.includes("{")) {
    writableBelow.set(parent, [...(writableBelow.get(parent) ?? []), name]);
  }
}

const samplePath = (template: string) => template.replace(/\{[^}]+\}/g, "x");

const linkedBy = (parent: string) =>
  editableByPath.find(([pattern]) => pattern.test(samplePath(parent)))?.[1];

// Only a detail resource links its sub-resources; a collection does not.
const detailParents = [...writableBelow].filter(
  ([parent]) => parent.endsWith("}") || linkedBy(parent) !== undefined,
);

describe("demo edit links", () => {
  it.each(detailParents)("%s links what the spec routes beneath it", (parent, names) => {
    expect(new Set(linkedBy(parent) ?? [])).toEqual(new Set(names));
  });

  it.each(editableByPath.map(([pattern]) => [pattern]))(
    "%s matches a path in the spec",
    (pattern) => {
      expect([...writableBelow.keys()].some((parent) => pattern.test(samplePath(parent)))).toBe(
        true,
      );
    },
  );
});
