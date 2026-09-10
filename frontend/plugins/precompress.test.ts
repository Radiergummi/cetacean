import { precompress } from "./precompress";
import { describe, expect, it } from "vitest";

function fakeBundle() {
  return {
    "assets/app.js": {
      type: "chunk" as const,
      fileName: "assets/app.js",
      code: "console.log('hello');".repeat(200),
      isEntry: true,
      imports: ["assets/vendor.js"],
    },
    "assets/tiny.js": {
      type: "chunk" as const,
      fileName: "assets/tiny.js",
      code: "0",
      isEntry: false,
      imports: [],
    },
    "assets/logo.png": {
      type: "asset" as const,
      fileName: "assets/logo.png",
      source: Buffer.alloc(4096),
    },
  };
}

describe("precompress", () => {
  it("emits gzip and zstd variants for compressible files", () => {
    const emitted: Array<{ fileName: string }> = [];
    const plugin = precompress();

    plugin.generateBundle.call(
      { emitFile: (file: { fileName: string }) => emitted.push(file) },
      {},
      fakeBundle(),
    );

    const names = emitted.map(({ fileName }) => fileName);
    expect(names).toContain("assets/app.js.gz");
    expect(names).toContain("assets/app.js.zst");
  });

  it("skips files below the threshold and already-compressed types", () => {
    const emitted: Array<{ fileName: string }> = [];
    precompress().generateBundle.call(
      { emitFile: (file: { fileName: string }) => emitted.push(file) },
      {},
      fakeBundle(),
    );

    const names = emitted.map(({ fileName }) => fileName);
    expect(names).not.toContain("assets/tiny.js.gz");
    expect(names).not.toContain("assets/logo.png.gz");
  });

  it("emits the manifest at the dist root, not under a dot-directory", () => {
    const emitted: Array<{ fileName: string; source: string }> = [];
    precompress().generateBundle.call(
      { emitFile: (file: { fileName: string; source: string }) => emitted.push(file) },
      {},
      fakeBundle(),
    );

    // go:embed frontend/dist/* excludes dot-prefixed directories with no
    // error at any stage, so the manifest must not live under .vite/.
    const manifest = emitted.find(({ fileName }) => fileName === "assets-manifest.json");
    expect(manifest).toBeDefined();
    expect(emitted.every(({ fileName }) => !fileName.startsWith("."))).toBe(true);

    const parsed = JSON.parse(manifest!.source);
    expect(parsed["assets/app.js"].variants.zstd.etag).toMatch(/^[0-9a-f]{32}$/);
    expect(parsed["assets/app.js"].entry).toBe(true);
  });
});
