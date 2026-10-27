import { stackNamespaceLabel } from "./parseStackLabels";
import { arrangeVersions, type Groupable } from "./versionGroups";
import { describe, expect, it } from "vitest";

function item(id: string, createdAt: string, versionOf?: string, stack?: string): Groupable {
  return {
    ID: id,
    CreatedAt: createdAt,
    UsedBy: 0,
    VersionOf: versionOf,
    Spec: { Labels: stack ? { [stackNamespaceLabel]: stack } : null },
  };
}

const summary = (rows: ReturnType<typeof arrangeVersions<Groupable>>) =>
  rows.map(({ ID, olderVersions, olderVersion }) => ({ ID, olderVersions, olderVersion }));

describe("arrangeVersions", () => {
  const items = [
    item("db_v1", "2026-01-01T00:00:00Z", "db"),
    item("api", "2026-01-01T00:00:00Z"),
    item("db_v3", "2026-01-03T00:00:00Z", "db"),
    item("db_v2", "2026-01-02T00:00:00Z", "db"),
  ];

  it("shows the newest version in place of its group, counting the older ones", () => {
    expect(summary(arrangeVersions(items, new Set()))).toEqual([
      { ID: "db_v3", olderVersions: 2, olderVersion: false },
      { ID: "api", olderVersions: 0, olderVersion: false },
    ]);
  });

  it("puts an expanded group's older versions beneath the newest, newest first", () => {
    const [newest] = arrangeVersions(items, new Set());
    const expanded = new Set([newest?.groupKey ?? ""]);

    expect(summary(arrangeVersions(items, expanded))).toEqual([
      { ID: "db_v3", olderVersions: 2, olderVersion: false },
      { ID: "db_v2", olderVersions: 0, olderVersion: true },
      { ID: "db_v1", olderVersions: 0, olderVersion: true },
      { ID: "api", olderVersions: 0, olderVersion: false },
    ]);
  });

  it("keeps groups of the same name in different stacks apart", () => {
    const stacked = [
      item("a_v1", "2026-01-01T00:00:00Z", "conf", "one"),
      item("b_v1", "2026-01-01T00:00:00Z", "conf", "two"),
    ];

    expect(summary(arrangeVersions(stacked, new Set()))).toEqual([
      { ID: "a_v1", olderVersions: 0, olderVersion: false },
      { ID: "b_v1", olderVersions: 0, olderVersion: false },
    ]);
  });

  it("breaks a creation tie by the greater ID", () => {
    const tied = [item("x", "2026-01-01T00:00:00Z", "s"), item("y", "2026-01-01T00:00:00Z", "s")];

    expect(arrangeVersions(tied, new Set())[0]?.ID).toBe("y");
  });
});
