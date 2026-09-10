import { createHash } from "node:crypto";
import { gzipSync, zstdCompressSync } from "node:zlib";
import type { Plugin, Rollup } from "vite";

/**
 * Minimum size, in bytes, a file must reach before it is worth compressing.
 * Mirrors the server's own compression threshold (internal/api/negotiate.go)
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
 * Options for the precompress plugin.
 */
export interface PrecompressOptions {
  /** Minimum size, in bytes, before a file is compressed. */
  thresholdBytes?: number;
  /** File extensions (without the leading dot) to never compress. */
  skippedExtensions?: readonly string[];
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
 */
export function precompress(options: PrecompressOptions = {}): Plugin {
  const thresholdBytes = options.thresholdBytes ?? defaultThresholdBytes;
  const skippedExtensions = options.skippedExtensions ?? defaultSkippedExtensions;

  return {
    name: "cetacean:precompress",
    apply: "build",
    enforce: "post",
    generateBundle(_outputOptions, bundle) {
      const manifest: Record<string, ManifestEntry> = {};

      // Snapshot the bundle before emitting: this hook's own emitFile calls
      // add to `bundle`, and iterating a live view would compress the
      // variants it just produced.
      const entries = Object.entries(bundle) as Array<
        [string, Rollup.OutputChunk | Rollup.OutputAsset]
      >;

      for (const [fileName, file] of entries) {
        const source = file.type === "chunk" ? file.code : file.source;
        const sourceBuffer = Buffer.from(source);

        if (sourceBuffer.byteLength < thresholdBytes) {
          continue;
        }
        if (skippedExtensions.includes(extensionOf(fileName))) {
          continue;
        }

        const gzip = gzipSync(sourceBuffer);
        const zstd = zstdCompressSync(sourceBuffer);

        this.emitFile({
          type: "asset",
          fileName: `${fileName}.gz`,
          source: gzip,
        });
        this.emitFile({
          type: "asset",
          fileName: `${fileName}.zst`,
          source: zstd,
        });

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

      this.emitFile({
        type: "asset",
        fileName: "assets-manifest.json",
        source: JSON.stringify(manifest, null, 2),
      });
    },
  };
}
