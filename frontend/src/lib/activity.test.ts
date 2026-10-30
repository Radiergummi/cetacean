import type { HistoryEntry } from "../api/types";
import { activityRows, hasChanges, keepChanges } from "./activity";
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

const shape = (entries: HistoryEntry[]) =>
  activityRows(entries).map((row) =>
    row.kind === "change" ? row.entry.id : row.gaps.map(({ id }) => id),
  );

describe("activityRows", () => {
  it("merges a run of gaps into one row between changes", () => {
    expect(shape([gap(6), change(5), gap(4), gap(3), change(2)])).toEqual([[6], 5, [4, 3], 2]);
  });

  it("drops gaps older than the oldest change", () => {
    expect(shape([change(3), gap(2), gap(1)])).toEqual([3]);
  });

  it("has no rows without a change", () => {
    expect(shape([gap(2), gap(1)])).toEqual([]);
  });

  it("skips kinds it does not know", () => {
    expect(shape([{ ...change(3), kind: "annotation" }, gap(2), change(1)])).toEqual([[2], 1]);
  });
});

describe("hasChanges", () => {
  it("counts changes only", () => {
    expect(hasChanges([gap(1)])).toBe(false);
    expect(hasChanges([gap(2), change(1)])).toBe(true);
  });
});

describe("keepChanges", () => {
  it("keeps the newest entries up to the limit-th change", () => {
    const entries = [gap(6), change(5), gap(4), change(3), gap(2), change(1)];

    expect(keepChanges(entries, 2).map(({ id }) => id)).toEqual([6, 5, 4, 3]);
  });

  it("keeps everything with fewer changes than the limit", () => {
    const entries = [gap(3), change(2), gap(1)];

    expect(keepChanges(entries, 10)).toEqual(entries);
  });
});
