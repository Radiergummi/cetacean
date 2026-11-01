import type { HistoryEntry } from "../api/types";
import { activityRows, hasChanges } from "./activity";
import { describe, expect, it } from "vitest";

const change = (id: number): HistoryEntry => ({
  id,
  kind: "change",
  timestamp: "2026-10-30T12:00:00Z",
  type: "service",
  action: "update",
  resourceId: "svc1",
  name: "web",
});

const gap = (id: number): HistoryEntry => ({
  id,
  kind: "gap",
  timestamp: "2026-10-30T13:00:00Z",
  until: "2026-10-30T14:00:00Z",
  type: "",
  action: "",
  resourceId: "",
  name: "",
});

const shape = (entries: HistoryEntry[], collapse = false) =>
  activityRows(entries, collapse).map((row) =>
    row.kind === "change" ? row.entries.map(({ id }) => id) : `gap ${row.gaps.map(({ id }) => id)}`,
  );

const alice = { subject: "u-1", name: "alice", provider: "oauth", transport: "mcp" } as const;

describe("activityRows", () => {
  it("merges a run of gaps into one row between changes", () => {
    expect(shape([gap(6), change(5), gap(4), gap(3), change(2)])).toEqual([
      "gap 6",
      [5],
      "gap 4,3",
      [2],
    ]);
  });

  it("drops gaps older than the oldest change", () => {
    expect(shape([change(3), gap(2), gap(1)])).toEqual([[3]]);
  });

  it("has no rows without a change", () => {
    expect(shape([gap(2), gap(1)])).toEqual([]);
  });

  it("skips kinds it does not know", () => {
    expect(shape([{ ...change(3), kind: "annotation" }, gap(2), change(1)])).toEqual([
      "gap 2",
      [1],
    ]);
  });

  it("keeps one row per change unless collapsing", () => {
    expect(shape([change(2), change(1)])).toEqual([[2], [1]]);
  });

  it("collapses consecutive changes to one resource with one action", () => {
    expect(shape([change(3), change(2), change(1)], true)).toEqual([[3, 2, 1]]);
  });

  it("ends a run at another resource, action or actor", () => {
    expect(
      shape(
        [
          change(6),
          { ...change(5), resourceId: "svc2" },
          change(4),
          { ...change(3), action: "remove" },
          change(2),
          { ...change(1), actor: alice },
        ],
        true,
      ),
    ).toEqual([[6], [5], [4], [3], [2], [1]]);
  });

  it("collapses changes by the same actor", () => {
    expect(
      shape(
        [
          { ...change(2), actor: alice },
          { ...change(1), actor: { ...alice } },
        ],
        true,
      ),
    ).toEqual([[2, 1]]);
  });

  it("ends a run at a gap", () => {
    expect(shape([change(3), gap(2), change(1)], true)).toEqual([[3], "gap 2", [1]]);
  });
});

describe("hasChanges", () => {
  it("counts changes only", () => {
    expect(hasChanges([gap(1)])).toBe(false);
    expect(hasChanges([gap(2), change(1)])).toBe(true);
  });
});
