import viteConfig from "./vite.config.ts";
import { describe, expect, it } from "vitest";

/**
 * Flattens viteConfig.plugins, whose entries are plugins or arrays of them,
 * into the names of every registered plugin.
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
    // Every other precompress test drives the plugin function directly, so a
    // dropped `precompress()` call here would only surface in a manual build.
    const names = flattenPluginNames(viteConfig.plugins);

    expect(names).toContain("cetacean:precompress");
  });
});
