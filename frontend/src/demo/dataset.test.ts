import { describe, expect, it, vi } from "vitest";

async function freshDataset() {
  vi.resetModules();
  const { buildDataset } = await import("./dataset");
  return buildDataset();
}

describe("buildDataset", () => {
  it("assigns the same IDs on every page load, so demo deep links survive a reload", async () => {
    const first = await freshDataset();
    const second = await freshDataset();

    for (const key of ["services", "nodes", "tasks", "configs", "secrets"] as const) {
      expect(second[key].map(({ ID }) => ID)).toEqual(first[key].map(({ ID }) => ID));
    }
    expect(second.networks.map(({ Id }) => Id)).toEqual(first.networks.map(({ Id }) => Id));
  });

  it("gives every task a distinct ID", async () => {
    const { tasks } = await freshDataset();

    expect(new Set(tasks.map(({ ID }) => ID)).size).toBe(tasks.length);
  });
});
