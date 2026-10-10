import { build, type Rollup } from "vite";
import { describe, expect, it } from "vitest";

describe("production build", () => {
  // The vendor groups exist to keep chart and graph code off routes that use neither.
  it("preloads neither the chart nor the topology bundle from the shell", async () => {
    const result = await build({
      root: import.meta.dirname,
      logLevel: "silent",
      build: { write: false },
    });
    const outputs = (Array.isArray(result) ? result : [result]).flatMap(
      (bundle) => (bundle as Rollup.RollupOutput).output,
    );
    const html = outputs.find(({ fileName }) => fileName === "index.html");

    expect(html?.type).toBe("asset");

    const preloads = String(html?.type === "asset" ? html.source : "").match(
      /rel="modulepreload"[^>]*href="[^"]+"/g,
    );

    expect(preloads).not.toBeNull();
    expect(preloads?.filter((link) => /vendor-(charts|topology|elk)/.test(link))).toEqual([]);
  }, 120_000);
});
