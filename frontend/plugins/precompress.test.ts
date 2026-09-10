import { precompress } from "./precompress";
import { createHash } from "node:crypto";
import { gzipSync, zstdCompressSync } from "node:zlib";
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

  it("compresses a file exactly at the 1024-byte threshold", () => {
    // The server's own skip condition (internal/api/negotiate.go) is a
    // strict less-than against the same 1024-byte threshold, so 1024 is
    // the smallest size either side compresses. Pinning it here keeps the
    // two from silently disagreeing about where the line falls.
    const bundle = {
      ...fakeBundle(),
      "assets/exact.js": {
        type: "chunk" as const,
        fileName: "assets/exact.js",
        code: "a".repeat(1024),
        isEntry: false,
        imports: [],
      },
    };

    const emitted: Array<{ fileName: string }> = [];
    precompress().generateBundle.call(
      { emitFile: (file: { fileName: string }) => emitted.push(file) },
      {},
      bundle,
    );

    const names = emitted.map(({ fileName }) => fileName);
    expect(names).toContain("assets/exact.js.gz");
    expect(names).toContain("assets/exact.js.zst");
  });
});

function digest(data: Uint8Array): string {
  return createHash("sha256").update(data).digest().subarray(0, 16).toString("hex");
}

describe("precompress ETag and size correctness", () => {
  it("derives each variant's ETag and size from its own bytes, not a swapped or shared value", () => {
    const emitted: Array<{ fileName: string; source: string }> = [];
    precompress().generateBundle.call(
      { emitFile: (file: { fileName: string; source: string }) => emitted.push(file) },
      {},
      fakeBundle(),
    );

    const manifest = emitted.find(({ fileName }) => fileName === "assets-manifest.json");
    const parsed = JSON.parse(manifest!.source);
    const entry = parsed["assets/app.js"];

    const originalSource = Buffer.from(fakeBundle()["assets/app.js"].code);
    const gzipBytes = gzipSync(originalSource);
    const zstdBytes = zstdCompressSync(originalSource);

    // A format check (/^[0-9a-f]{32}$/) passes even if the gzip and zstd
    // digests were swapped, or if a variant's ETag were computed over the
    // original bytes instead of its own compressed bytes. Recomputing each
    // digest independently is what actually catches that.
    expect(entry.etag).toBe(digest(originalSource));
    expect(entry.variants.gzip.etag).toBe(digest(gzipBytes));
    expect(entry.variants.zstd.etag).toBe(digest(zstdBytes));

    // Belt-and-suspenders: the three digests must be pairwise distinct, so
    // a swap between them cannot coincidentally still satisfy the equality
    // checks above.
    const etags = [entry.etag, entry.variants.gzip.etag, entry.variants.zstd.etag];
    expect(new Set(etags).size).toBe(3);

    expect(entry.size).toBe(originalSource.byteLength);
    expect(entry.variants.gzip.size).toBe(gzipBytes.byteLength);
    expect(entry.variants.zstd.size).toBe(zstdBytes.byteLength);
  });
});
