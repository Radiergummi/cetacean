import { columnsFor } from "./columns";
import { describe, expect, it } from "vitest";

describe("columnsFor", () => {
  it("reads a compact row without per-type accessors", () => {
    const row = {
      id: "svc1",
      name: "demo_web",
      type: "service",
      stack: "demo",
      state: "running",
      detail: "nginx:alpine",
      desired: 2,
      running: 2,
    };

    const rendered = columnsFor("services").map(({ key, value }) => [key, value(row)]);

    expect(Object.fromEntries(rendered)).toMatchObject({
      name: "demo_web",
      state: "running",
      detail: "nginx:alpine",
    });
  });
});
