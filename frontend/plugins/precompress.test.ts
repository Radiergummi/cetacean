import { precompress } from "./precompress";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { gzipSync, zstdCompressSync, zstdDecompressSync } from "node:zlib";
import { describe, expect, it } from "vitest";

interface FakeFile {
  type: "chunk" | "asset";
  isEntry?: boolean;
  /** What Vite wrote to disk, which is what the server will serve. */
  contents: string | Buffer;
  /**
   * What the in-memory bundle record still claims, where that has gone stale.
   * Vite's own post plugins run after user ones and rewrite chunk code in
   * their generateBundle, so a value read out of the bundle can differ from
   * the file that ends up on disk. Setting this proves the plugin reads the
   * file rather than the record.
   */
  bundleCode?: string;
}

/**
 * Writes each file into a fresh output directory, runs the plugin's
 * writeBundle over a bundle describing them, and returns that directory.
 */
function runPrecompress(files: Record<string, FakeFile>): string {
  const outDir = mkdtempSync(join(tmpdir(), "precompress-"));
  const bundle: Record<string, unknown> = {};

  for (const [fileName, file] of Object.entries(files)) {
    const filePath = join(outDir, fileName);
    mkdirSync(dirname(filePath), { recursive: true });
    writeFileSync(filePath, file.contents);

    bundle[fileName] =
      file.type === "chunk"
        ? {
            type: "chunk",
            fileName,
            code: file.bundleCode ?? file.contents.toString(),
            isEntry: file.isEntry ?? false,
            imports: [],
          }
        : { type: "asset", fileName, source: file.contents };
  }

  precompress().writeBundle.call(
    {
      error: (message: string) => {
        throw new Error(message);
      },
    },
    { dir: outDir },
    bundle,
  );

  return outDir;
}

function readManifest(outDir: string): Record<string, never> {
  return JSON.parse(readFileSync(join(outDir, "assets-manifest.json"), "utf8"));
}

function fakeFiles(): Record<string, FakeFile> {
  return {
    "assets/app.js": {
      type: "chunk",
      isEntry: true,
      contents: "console.log('hello');".repeat(200),
    },
    "assets/tiny.js": { type: "chunk", contents: "0" },
    "assets/logo.png": { type: "asset", contents: Buffer.alloc(4096) },
    "index.html": { type: "asset", contents: "<!doctype html>".repeat(200) },
  };
}

function digest(data: Uint8Array): string {
  return createHash("sha256").update(data).digest().subarray(0, 16).toString("hex");
}

describe("precompress", () => {
  it("emits gzip and zstd variants for compressible files", () => {
    const outDir = runPrecompress(fakeFiles());

    expect(existsSync(join(outDir, "assets/app.js.gz"))).toBe(true);
    expect(existsSync(join(outDir, "assets/app.js.zst"))).toBe(true);
  });

  it("skips files below the threshold and already-compressed types", () => {
    const outDir = runPrecompress(fakeFiles());

    expect(existsSync(join(outDir, "assets/tiny.js.gz"))).toBe(false);
    expect(existsSync(join(outDir, "assets/logo.png.gz"))).toBe(false);

    const manifest = readManifest(outDir);
    expect(manifest).not.toHaveProperty("assets/tiny.js");
    expect(manifest).not.toHaveProperty("assets/logo.png");
  });

  it("leaves index.html alone", () => {
    // NewSPAHandler intercepts index.html and answers with a copy carrying an
    // injected <base href>, so a build-time variant could never match the
    // bytes served — and /index.html.gz, absent from the manifest, would fall
    // through to serveAsset and hand out the un-injected shell.
    const outDir = runPrecompress(fakeFiles());

    expect(existsSync(join(outDir, "index.html.gz"))).toBe(false);
    expect(existsSync(join(outDir, "index.html.zst"))).toBe(false);
    expect(readManifest(outDir)).not.toHaveProperty("index.html");
  });

  it("compresses what is on disk, not what the bundle record still says", () => {
    // The defect this plugin was rewritten for: hooked into generateBundle,
    // it compressed chunks before vite:build-import-analysis had rewritten
    // their __VITE_PRELOAD__ markers, shipping variants that threw
    // ReferenceError in every browser that negotiated one.
    const written = "__vite__mapDeps([0,1]);".repeat(100);
    const outDir = runPrecompress({
      "assets/app.js": {
        type: "chunk",
        isEntry: true,
        contents: written,
        bundleCode: "__VITE_PRELOAD__;".repeat(100),
      },
    });

    const roundTripped = zstdDecompressSync(
      readFileSync(join(outDir, "assets/app.js.zst")),
    ).toString();
    expect(roundTripped).toBe(written);
    expect(readManifest(outDir)["assets/app.js"]).toMatchObject({
      etag: digest(Buffer.from(written)),
    });
  });

  it("writes the manifest at the dist root, not under a dot-directory", () => {
    // go:embed frontend/dist/* excludes dot-prefixed directories with no
    // error at any stage, so the manifest must not live under .vite/.
    const outDir = runPrecompress(fakeFiles());
    const manifest = readManifest(outDir);

    expect(existsSync(join(outDir, "assets-manifest.json"))).toBe(true);
    expect(Object.keys(manifest).every((fileName) => !fileName.startsWith("."))).toBe(true);
    expect(manifest["assets/app.js"]).toMatchObject({ entry: true });
  });

  it("compresses a file exactly at the 1024-byte threshold", () => {
    // The server's own skip condition (internal/api/encoding.go) is a strict
    // less-than against the same 1024-byte threshold, so 1024 is the smallest
    // size either side compresses. Pinning it here keeps the two from
    // silently disagreeing about where the line falls.
    const outDir = runPrecompress({
      "assets/exact.js": { type: "chunk", contents: "a".repeat(1024) },
    });

    expect(existsSync(join(outDir, "assets/exact.js.gz"))).toBe(true);
    expect(existsSync(join(outDir, "assets/exact.js.zst"))).toBe(true);
  });
});

describe("precompress ETag and size correctness", () => {
  it("derives each variant's ETag and size from its own bytes, not a swapped or shared value", () => {
    const outDir = runPrecompress(fakeFiles());
    const entry = readManifest(outDir)["assets/app.js"] as unknown as {
      etag: string;
      size: number;
      variants: Record<"gzip" | "zstd", { etag: string; size: number }>;
    };

    const originalSource = Buffer.from(fakeFiles()["assets/app.js"].contents);
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

    // And the digests must describe the files actually written beside them.
    expect(digest(readFileSync(join(outDir, "assets/app.js.gz")))).toBe(entry.variants.gzip.etag);
    expect(digest(readFileSync(join(outDir, "assets/app.js.zst")))).toBe(entry.variants.zstd.etag);
  });
});
