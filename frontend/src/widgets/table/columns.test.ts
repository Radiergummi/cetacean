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

  // find omits a false newestVersion; the column still says "no", as the CSV does.
  it("names an older version as not the newest", () => {
    const [older, newest] = [
      { id: "s1", name: "db_v1", type: "secret", versionOf: "db", usedBy: 0 },
      { id: "s2", name: "db_v2", type: "secret", versionOf: "db", newestVersion: true, usedBy: 1 },
    ].map((row) =>
      Object.fromEntries(columnsFor("secrets").map(({ key, value }) => [key, value(row)])),
    );

    expect(older?.newestVersion).toBe("no");
    expect(newest?.newestVersion).toBe("yes");
  });
});
