import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { gzipSync, zstdCompressSync } from "node:zlib";
import type { Plugin, Rollup } from "vite";

/**
 * Minimum size, in bytes, a file must reach before it is worth compressing.
 * Mirrors the server's own compression threshold (internal/api/encoding.go)
 * so a build never precompresses something the server would have served
 * uncompressed anyway.
 */
const defaultThresholdBytes = 1024;

/**
 * File extensions that are already compressed, so gzipping or zstd-ing them
 * again wastes a build cycle for a larger, not smaller, variant.
 */
const defaultSkippedExtensions = ["png", "ico", "woff2", "woff", "jpg", "webp", "avif"] as const;

/**
 * Files the server never serves from disk, so a build-time variant of them is
 * dead weight at best. NewSPAHandler intercepts index.html and answers with a
 * copy carrying an injected <base href> and <link rel=canonical>, which are
 * different bytes from the ones built here — and an index.html.gz reachable
 * over HTTP would hand out the un-injected shell.
 */
const defaultSkippedFileNames = ["index.html"] as const;

/**
 * Options for the precompress plugin.
 */
export interface PrecompressOptions {
  /** Minimum size, in bytes, before a file is compressed. */
  thresholdBytes?: number;
  /** File extensions (without the leading dot) to never compress. */
  skippedExtensions?: readonly string[];
  /** Output-relative file names to never compress. */
  skippedFileNames?: readonly string[];
}

interface ManifestVariant {
  size: number;
  etag: string;
}

interface ManifestEntry {
  size: number;
  etag: string;
  entry: boolean;
  variants: Record<"gzip" | "zstd", ManifestVariant>;
}

/**
 * computeETag mirrors Go's computeETag (internal/api/etag.go): the SHA-256
 * of the bytes, truncated to 16 bytes and hex-encoded. Go's version wraps
 * the result in double quotes to form an HTTP ETag; this manifest stores
 * the bare hex digest instead, and it is Task 15's Go code that adds the
 * quotes when turning a manifest entry into a response header. Adding
 * quotes here would double-quote the value there, so this asymmetry is
 * deliberate, not an oversight.
 */
function computeETag(data: Uint8Array): string {
  return createHash("sha256").update(data).digest().subarray(0, 16).toString("hex");
}

function extensionOf(fileName: string): string {
  const dotIndex = fileName.lastIndexOf(".");
  if (dotIndex === -1) {
    return "";
  }

  return fileName.slice(dotIndex + 1).toLowerCase();
}

/**
 * precompress emits gzip and zstd variants of every chunk and asset in the
 * build output, plus an assets-manifest.json describing them (size and ETag
 * per representation). Go's SPA handler reads the manifest at startup to
 * serve a precompressed variant instead of compressing on every request.
 *
 * Files under the size threshold, or with an already-compressed extension,
 * are left alone and do not appear in the manifest.
 *
 * The work happens in writeBundle, reading each file back from the output
 * directory rather than out of the in-memory bundle, because the bundle is
 * not final until every generateBundle hook has run — and Vite orders its own
 * internal post plugins after user `enforce: "post"` ones, so
 * vite:build-import-analysis rewrites the __VITE_PRELOAD__ marker into
 * __vite__mapDeps(...) after a plugin like this one would have seen it.
 * Compressing that snapshot shipped variants referencing an undefined
 * identifier, which every browser reached before the identity file. What is
 * on disk when writeBundle runs is what the server will serve, so that is
 * what gets hashed and compressed.
 */
export function precompress(options: PrecompressOptions = {}): Plugin {
  const thresholdBytes = options.thresholdBytes ?? defaultThresholdBytes;
  const skippedExtensions = options.skippedExtensions ?? defaultSkippedExtensions;
  const skippedFileNames = options.skippedFileNames ?? defaultSkippedFileNames;

  return {
    name: "cetacean:precompress",
    apply: "build",
    enforce: "post",
    writeBundle(outputOptions, bundle) {
      const outDir = outputOptions.dir;
      if (!outDir) {
        this.error("cetacean:precompress requires a directory output, but none was configured");
      }

      const manifest: Record<string, ManifestEntry> = {};

      const entries = Object.entries(bundle) as Array<
        [string, Rollup.OutputChunk | Rollup.OutputAsset]
      >;

      for (const [fileName, file] of entries) {
        if (skippedFileNames.includes(fileName)) {
          continue;
        }
        if (skippedExtensions.includes(extensionOf(fileName))) {
          continue;
        }

        const filePath = join(outDir, fileName);
        const sourceBuffer = readFileSync(filePath);

        if (sourceBuffer.byteLength < thresholdBytes) {
          continue;
        }

        const gzip = gzipSync(sourceBuffer);
        const zstd = zstdCompressSync(sourceBuffer);

        writeFileSync(`${filePath}.gz`, gzip);
        writeFileSync(`${filePath}.zst`, zstd);

        manifest[fileName] = {
          size: sourceBuffer.byteLength,
          etag: computeETag(sourceBuffer),
          entry: file.type === "chunk" && file.isEntry,
          variants: {
            gzip: { size: gzip.byteLength, etag: computeETag(gzip) },
            zstd: { size: zstd.byteLength, etag: computeETag(zstd) },
          },
        };
      }

      writeFileSync(join(outDir, "assets-manifest.json"), JSON.stringify(manifest, null, 2));
    },
  };
}
