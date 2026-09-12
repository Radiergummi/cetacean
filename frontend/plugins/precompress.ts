import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { gzipSync, zstdCompressSync } from "node:zlib";
import type { Plugin, Rollup } from "vite";

/**
 * Minimum size, in bytes, a file must reach before it is worth compressing.
 * Mirrors the server's threshold in internal/api/encoding.go.
 */
const defaultThresholdBytes = 1024;

/**
 * File extensions that are already compressed, where a variant would be larger.
 */
const defaultSkippedExtensions = ["png", "ico", "woff2", "woff", "jpg", "webp", "avif"] as const;

/**
 * Files the server never serves from disk. NewSPAHandler answers index.html
 * with an injected copy, so an index.html.gz would hand out the un-injected
 * shell.
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
 * computeETag mirrors Go's computeETag (internal/api/etag.go): SHA-256 of the
 * bytes, truncated to 16 and hex-encoded. The manifest stores the bare digest;
 * the Go handler adds the quotes, and adding them here would double them.
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
 * build output, plus an assets-manifest.json describing them (size and ETag per
 * representation), which Go's SPA handler reads at startup. Files under the
 * threshold, or with an already-compressed extension, are left out of both.
 *
 * The work happens in writeBundle, reading each file back from the output
 * directory rather than from the in-memory bundle: Vite orders its own internal
 * post plugins after user `enforce: "post"` ones, so vite:build-import-analysis
 * rewrites __VITE_PRELOAD__ after a generateBundle hook here would have seen it,
 * and the variants would reference an undefined identifier.
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
