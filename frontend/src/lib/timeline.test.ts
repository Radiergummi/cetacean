import type { HistoryEntry } from "../api/types";
import { formatDateTime } from "./format";
import { describeAnnotationTime, describeChange, describeGap, timelineItems } from "./timeline";
import { describe, expect, it } from "vitest";

const entry = (overrides: Partial<HistoryEntry>): HistoryEntry => ({
  id: 1,
  kind: "change",
  timestamp: "2026-10-30T12:00:00Z",
  type: "service",
  action: "update",
  resourceId: "svc1",
  name: "web",
  ...overrides,
});

const at = (timestamp: string) => new Date(timestamp).getTime() / 1000;

describe("timelineItems", () => {
  it("keeps the resource's own changes as points and gaps as ranges", () => {
    const items = timelineItems(
      [
        entry({ id: 3 }),
        entry({
          id: 2,
          kind: "gap",
          timestamp: "2026-10-30T10:00:00Z",
          until: "2026-10-30T11:00:00Z",
          type: "",
        }),
      ],
      "service",
    );

    expect(items).toEqual([
      expect.objectContaining({ shape: "point", kind: "change", at: at("2026-10-30T12:00:00Z") }),
      expect.objectContaining({
        shape: "range",
        kind: "gap",
        from: at("2026-10-30T10:00:00Z"),
        to: at("2026-10-30T11:00:00Z"),
      }),
    ]);
  });

  it("drops a service's task entries", () => {
    expect(timelineItems([entry({ type: "task", resourceId: "t1" })], "service")).toEqual([]);
  });

  it("skips kinds it does not know and gaps without an end", () => {
    expect(
      timelineItems([entry({ kind: "note" }), entry({ kind: "gap", type: "" })], "service"),
    ).toEqual([]);
  });
});

describe("timelineItems on annotations", () => {
  const note = (overrides: Partial<HistoryEntry>) =>
    entry({ kind: "annotation", type: "", action: "", resourceId: "", name: "", ...overrides });

  it("keeps annotations, whatever their type", () => {
    const point = note({ id: 4 });
    const range = note({ id: 5, until: "2026-10-30T13:00:00Z" });

    expect(timelineItems([point, range], "service")).toEqual([
      { shape: "point", kind: "annotation", at: at("2026-10-30T12:00:00Z"), entry: point },
      {
        shape: "range",
        kind: "annotation",
        from: at("2026-10-30T12:00:00Z"),
        to: at("2026-10-30T13:00:00Z"),
        entry: range,
      },
    ]);
  });

  it("drops an annotation whose range does not end after it starts", () => {
    expect(timelineItems([note({ until: "2026-10-30T12:00:00Z" })], "service")).toEqual([]);
  });
});

describe("describeAnnotationTime", () => {
  const day = { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" } as const;
  const clock = { hour: "2-digit", minute: "2-digit" } as const;
  const local = (hours: number, minutes = 0, date = 1) =>
    new Date(2026, 10, date, hours, minutes).getTime() / 1000;
  const show = (seconds: number, options: Intl.DateTimeFormatOptions) =>
    formatDateTime(new Date(seconds * 1000), options);

  it("describes a point", () => {
    expect(describeAnnotationTime(local(14))).toBe(`about ${show(local(14), day)}`);
  });

  it("gives a range on one day one date", () => {
    expect(describeAnnotationTime(local(14), local(14, 30))).toBe(
      `about ${show(local(14), day)}–${show(local(14, 30), clock)}`,
    );
  });

  it("dates both ends of a range across midnight", () => {
    expect(describeAnnotationTime(local(23, 30), local(0, 30, 2))).toBe(
      `about ${show(local(23, 30), day)} – ${show(local(0, 30, 2), day)}`,
    );
  });
});

describe("describeChange", () => {
  it("names the actor when Cetacean knows it", () => {
    expect(
      describeChange(
        entry({ actor: { subject: "u1", name: "alice", provider: "oidc", transport: "mcp" } }),
      ),
    ).toBe("Updated by alice via MCP");
  });

  it("says only what happened for a change made elsewhere", () => {
    expect(describeChange(entry({ action: "remove" }))).toBe("Removed");
  });
});

describe("describeGap", () => {
  it("reads as not watching, not as quiet", () => {
    expect(describeGap(0, 3 * 3600).title).toBe("Not watching for 3h");
  });
});
