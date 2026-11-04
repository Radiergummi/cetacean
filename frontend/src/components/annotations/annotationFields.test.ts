import {
  annotationBody,
  annotationInputs,
  annotationPatch,
  changedOnlyByRemovals,
  chartPath,
  noteSegments,
  fromLocalInput,
  parseTag,
  toLocalInput,
} from "./annotationFields";
import type { Annotation } from "@/api/types";
import { describe, expect, it } from "vitest";

describe("parseTag", () => {
  it("keeps a tag the API takes, lowercased", () => {
    expect(parseTag(" Deploy ")).toBe("deploy");
    expect(parseTag("load-test.v2_a")).toBe("load-test.v2_a");
  });

  it("refuses one it does not", () => {
    expect(parseTag("a b")).toBeNull();
    expect(parseTag("")).toBeNull();
    expect(parseTag("x".repeat(41))).toBeNull();
    expect(parseTag("Bad!")).toBeNull();
  });
});

describe("local times", () => {
  it("round-trips a whole minute to the same instant in UTC", () => {
    const at = Date.UTC(2026, 10, 2, 13, 37) / 1000;

    expect(toLocalInput(at)).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/);
    expect(fromLocalInput(toLocalInput(at))).toBe(new Date(at * 1000).toISOString());
  });

  it("drops the seconds of a time it shows", () => {
    const at = Date.UTC(2026, 10, 2, 13, 37, 42) / 1000;

    expect(fromLocalInput(toLocalInput(at))).toBe(new Date((at - 42) * 1000).toISOString());
  });
});

describe("annotationBody", () => {
  const from = toLocalInput(Date.UTC(2026, 10, 2, 13, 0) / 1000);
  const to = toLocalInput(Date.UTC(2026, 10, 2, 14, 0) / 1000);
  const service = { type: "service", id: "svc1", name: "web" } as const;

  it("names the resource it is about", () => {
    expect(annotationBody({ text: "x", tags: ["a"], from, to, scope: service })).toEqual({
      text: "x",
      tags: ["a"],
      timestamp: "2026-11-02T13:00:00.000Z",
      until: "2026-11-02T14:00:00.000Z",
      resources: [{ type: "service", id: "svc1" }],
    });
  });

  it("names nothing when cluster-wide, and has no end without one", () => {
    expect(annotationBody({ text: "x", tags: [], from, to: "", scope: "cluster" })).toEqual({
      text: "x",
      tags: [],
      timestamp: "2026-11-02T13:00:00.000Z",
    });
  });
});

describe("annotationPatch", () => {
  const note: Annotation = {
    id: 7,
    timestamp: "2026-11-02T13:00:30Z",
    until: "2026-11-02T14:00:00Z",
    text: "load test",
    tags: ["load"],
    resources: [],
  };
  const inputs = annotationInputs(note);

  it("fills the form from a note, to the minute", () => {
    expect(inputs).toEqual({
      text: "load test",
      tags: ["load"],
      from: toLocalInput(Date.UTC(2026, 10, 2, 13, 0) / 1000),
      to: toLocalInput(Date.UTC(2026, 10, 2, 14, 0) / 1000),
    });
    expect(annotationInputs({ ...note, until: undefined }).to).toBe("");
  });

  it("sends nothing when nothing changed, so the seconds of the start survive", () => {
    expect(annotationPatch(note, inputs)).toEqual({});
  });

  it("sends only what changed", () => {
    const later = toLocalInput(Date.UTC(2026, 10, 2, 13, 30) / 1000);

    expect(annotationPatch(note, { ...inputs, text: "soak test", from: later })).toEqual({
      text: "soak test",
      timestamp: "2026-11-02T13:30:00.000Z",
    });
    expect(annotationPatch(note, { ...inputs, tags: ["load", "soak"] })).toEqual({
      tags: ["load", "soak"],
    });
  });

  it("clears the end with null, and an emptied tag list with an empty one", () => {
    expect(annotationPatch(note, { ...inputs, to: "", tags: [] })).toEqual({
      until: null,
      tags: [],
    });
  });

  it("gives a point an end", () => {
    const point = { ...note, until: undefined };
    const to = toLocalInput(Date.UTC(2026, 10, 2, 15, 0) / 1000);

    expect(annotationPatch(point, { ...annotationInputs(point), to })).toEqual({
      until: "2026-11-02T15:00:00.000Z",
    });
  });
});

describe("changedOnlyByRemovals", () => {
  const opened: Annotation = {
    id: 7,
    timestamp: "2026-11-02T13:00:00Z",
    text: "Load test",
    tags: [],
    resources: [{ type: "service", id: "svc1", name: "web" }],
  };

  it("takes a resource marked removed as no change", () => {
    const current = { ...opened, resources: [{ ...opened.resources[0]!, removed: true }] };

    expect(changedOnlyByRemovals(opened, current)).toBe(true);
  });

  it("takes an edit as a change", () => {
    expect(changedOnlyByRemovals(opened, { ...opened, text: "Soak test" })).toBe(false);
  });
});

describe("noteSegments", () => {
  it("splits the URLs out of the text", () => {
    expect(noteSegments("see https://example.com/runbook for the steps")).toEqual([
      { text: "see " },
      { text: "https://example.com/runbook", url: "https://example.com/runbook" },
      { text: " for the steps" },
    ]);
  });

  it("leaves a sentence's punctuation after the URL", () => {
    expect(noteSegments("(see http://a.example/x).")).toEqual([
      { text: "(see " },
      { text: "http://a.example/x", url: "http://a.example/x" },
      { text: ")." },
    ]);
  });

  it("keeps a parenthesis the URL opened", () => {
    const url = "https://en.wikipedia.org/wiki/Swarm_(disambiguation)";

    expect(noteSegments(`${url}, then`)).toEqual([{ text: url, url }, { text: ", then" }]);
  });

  it("links only http and https", () => {
    expect(noteSegments("javascript:alert(1) and ftp://host/file")).toEqual([
      { text: "javascript:alert(1) and ftp://host/file" },
    ]);
  });
});

describe("chartPath", () => {
  const at = Date.UTC(2026, 10, 2, 13, 0) / 1000;
  const timestamp = new Date(at * 1000).toISOString();
  const now = at + 86_400;

  it("opens the first service or node with a metrics panel, skipping stacks and removed ones", () => {
    expect(
      chartPath(
        {
          timestamp,
          resources: [
            { type: "stack", id: "shop", name: "shop" },
            { type: "service", id: "gone", name: "old", removed: true },
            { type: "node", id: "n1", name: "worker" },
          ],
        },
        now,
      ),
    ).toBe(`/nodes/n1?from=${at - 1800}&to=${at + 1800}`);
  });

  it("widens around a long range and stops at now", () => {
    const until = new Date((at + 8 * 3600) * 1000).toISOString();

    expect(
      chartPath(
        { timestamp, until, resources: [{ type: "service", id: "s1", name: "web" }] },
        at + 9 * 3600,
      ),
    ).toBe(`/services/s1?from=${at - 2 * 3600}&to=${at + 9 * 3600}`);
  });

  it("has nowhere to go for a cluster-wide note or one yet to come", () => {
    expect(chartPath({ timestamp, resources: [] }, now)).toBeUndefined();
    expect(
      chartPath({ timestamp, resources: [{ type: "node", id: "n1", name: "worker" }] }, at - 60),
    ).toBeUndefined();
  });
});
