import type { HistoryEntry } from "../api/types";
import { describeChange, describeGap, timelineItems } from "./timeline";
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
      timelineItems([entry({ kind: "annotation" }), entry({ kind: "gap", type: "" })], "service"),
    ).toEqual([]);
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
