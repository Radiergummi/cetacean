import type { HistoryEntry } from "../api/types";
import { activityRows, hasActivity } from "./activity";
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

const annotation = (id: number): HistoryEntry => ({
  ...gap(id),
  kind: "annotation",
  until: undefined,
  text: "note",
});

const shape = (entries: HistoryEntry[], collapse = false) =>
  activityRows(entries, collapse).map((row) => {
    if (row.kind === "change") {
      return row.entries.map(({ id }) => id);
    }

    return row.kind === "annotation"
      ? `annotation ${row.entry.id}`
      : `gap ${row.gaps.map(({ id }) => id)}`;
  });

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
    expect(shape([{ ...change(3), kind: "note" }, gap(2), change(1)])).toEqual(["gap 2", [1]]);
  });

  it("keeps one row per change unless collapsing", () => {
    expect(shape([change(2), change(1)])).toEqual([[2], [1]]);
  });

  it("collapses consecutive changes to one resource with one action", () => {
    expect(shape([change(3), change(2), change(1)], true)).toEqual([[3, 2, 1]]);
  });

  it("collapses changes to different resources that read the same", () => {
    expect(
      shape(
        [
          { ...change(2), type: "task", resourceId: "task-new", name: "web.2" },
          { ...change(1), type: "task", resourceId: "task-old", name: "web.2" },
        ],
        true,
      ),
    ).toEqual([[2, 1]]);
  });

  it("folds one resource's changes whatever the action", () => {
    expect(
      shape(
        [change(3), { ...change(2), action: "create" }, { ...change(1), action: "remove" }],
        true,
      ),
    ).toEqual([[3, 2, 1]]);
  });

  it("ends a run at another name or actor", () => {
    expect(
      shape(
        [change(4), { ...change(3), name: "api" }, change(2), { ...change(1), actor: alice }],
        true,
      ),
    ).toEqual([[4], [3], [2], [1]]);
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

describe("activityRows on annotations", () => {
  it("gives an annotation its own row and ends a run at it", () => {
    expect(shape([change(5), annotation(4), change(3)], true)).toEqual([[5], "annotation 4", [3]]);
  });

  it("keeps gaps an annotation frames and drops those older than it", () => {
    expect(shape([gap(5), annotation(4), gap(3), change(2), gap(1)])).toEqual([
      "gap 5",
      "annotation 4",
      "gap 3",
      [2],
    ]);
    expect(shape([gap(3), annotation(2), gap(1)])).toEqual(["gap 3", "annotation 2"]);
  });
});

describe("hasActivity", () => {
  it("counts changes and annotations, not gaps", () => {
    expect(hasActivity([gap(1)])).toBe(false);
    expect(hasActivity([gap(2), change(1)])).toBe(true);
    expect(hasActivity([annotation(1), gap(2)])).toBe(true);
  });
});
