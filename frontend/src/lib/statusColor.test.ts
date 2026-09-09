import { replicaHealthColor, statusColor, statusTone, toneSurface } from "./statusColor";
import { describe, it, expect } from "vitest";

describe("statusTone", () => {
  it("groups the healthy states", () => {
    for (const state of ["running", "ready", "complete"]) {
      expect(statusTone(state)).toBe("ok");
    }
  });

  it("groups the failure states", () => {
    for (const state of ["failed", "rejected", "down", "orphaned"]) {
      expect(statusTone(state)).toBe("danger");
    }
  });

  it("groups the in-flight states", () => {
    for (const state of ["preparing", "starting", "pending", "assigned", "accepted"]) {
      expect(statusTone(state)).toBe("warning");
    }
  });

  it("falls back to neutral for a state it does not know", () => {
    expect(statusTone("unknown")).toBe("neutral");
    expect(statusTone("shutdown")).toBe("neutral");
  });
});

describe("statusColor", () => {
  it("resolves through the semantic token, not a palette shade", () => {
    expect(statusColor("running")).toBe("bg-status-ok");
    expect(statusColor("failed")).toBe("bg-status-danger");
    expect(statusColor("starting")).toBe("bg-status-warning");
    expect(statusColor("unknown")).toBe("bg-status-neutral");
  });

  // The token carries its own dark value, so no call site pairs a `dark:`
  // variant with it any more.
  it("needs no dark-mode variant", () => {
    for (const state of ["running", "failed", "starting", "unknown"]) {
      expect(statusColor(state)).not.toContain("dark:");
    }
  });
});

describe("replicaHealthColor", () => {
  it("reads all-running as ok", () => {
    expect(replicaHealthColor(3, 3)).toBe("bg-status-ok");
    expect(replicaHealthColor(0, 0)).toBe("bg-status-ok");
  });

  it("reads partial as warning and none as danger", () => {
    expect(replicaHealthColor(1, 3)).toBe("bg-status-warning");
    expect(replicaHealthColor(0, 3)).toBe("bg-status-danger");
  });
});

describe("toneSurface", () => {
  it("tints the same token it colours the text with", () => {
    for (const tone of ["ok", "warning", "danger", "neutral"] as const) {
      expect(toneSurface[tone]).toBe(`bg-status-${tone}/15 text-status-${tone}`);
    }
  });
});

describe("status tint call sites", () => {
  /**
   * The status sweep rewrote pairs like `bg-yellow-500/5 … text-yellow-600`
   * onto the shared tokens and dropped the opacity modifier, which left amber
   * text sitting on solid amber in two warning banners, the plugin pill and the
   * engine badge. An element that carries both a status background and status
   * text of the same tone has to tint the background, or it paints its own text
   * out. This reads one class list at a time, so it says nothing about a card
   * that colours its background and its number from separate elements — the
   * cluster overview does that, and only a browser catches it.
   */
  it("never paints status text on a solid background of the same tone", async () => {
    const { readdirSync, readFileSync, statSync } = await import("node:fs");
    const { join } = await import("node:path");

    const sources: string[] = [];
    const walk = (directory: string) => {
      for (const entry of readdirSync(directory)) {
        const path = join(directory, entry);

        if (statSync(path).isDirectory()) {
          walk(path);
          continue;
        }

        if (/\.tsx?$/.test(path) && !/\.test\.tsx?$/.test(path)) {
          sources.push(path);
        }
      }
    };

    walk("src");

    const offenders: string[] = [];
    for (const path of sources) {
      const source = readFileSync(path, "utf8");

      for (const [classList] of source.matchAll(/"([^"\n]*status-[^"\n]*)"/g)) {
        for (const tone of ["ok", "warning", "danger", "info", "neutral"]) {
          const solidBackground = new RegExp(`bg-status-${tone}(?![\\w/-])`);
          const tonedText = new RegExp(`text-status-${tone}(?![\\w-])`);

          if (solidBackground.test(classList) && tonedText.test(classList)) {
            offenders.push(`${path}: ${classList.trim().slice(0, 60)}`);
          }
        }
      }
    }

    expect(offenders).toEqual([]);
  });
});
