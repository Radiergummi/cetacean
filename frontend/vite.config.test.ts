import viteConfig from "./vite.config.ts";
import { describe, expect, it } from "vitest";

/**
 * viteConfig.plugins is an array whose entries are themselves plugins or
 * arrays of plugins — @vitejs/plugin-react and @tailwindcss/vite each
 * return several. This flattens one level of nesting deep enough to find
 * every registered plugin object by name.
 */
function flattenPluginNames(plugins: unknown): Array<string | undefined> {
  if (Array.isArray(plugins)) {
    return plugins.flatMap((plugin) => flattenPluginNames(plugin));
  }

  if (plugins && typeof plugins === "object" && "name" in plugins) {
    return [(plugins as { name?: string }).name];
  }

  return [];
}

describe("vite.config plugin registration", () => {
  it("registers the precompress plugin", () => {
    // Nothing else in this project's test suite touches vite.config.ts
    // itself — every precompress test drives the plugin function directly
    // with a fake bundle. That leaves a dropped `precompress()` call in the
    // `plugins` array (e.g. lost in a merge conflict) invisible to
    // `npx vitest run`, caught only by a manual `npm run build`, which CI
    // does not repeat for this project. This test closes that gap.
    const names = flattenPluginNames(viteConfig.plugins);

    expect(names).toContain("cetacean:precompress");
  });
});
