import type { PatchOp } from "@/api/types";

/**
 * Compute JSON Patch operations from label changes.
 *
 * Compares `newLabels` against the original raw label entries for an integration.
 * Keys present in `originalEntries` but absent from `newLabels` are removed.
 * Keys not in either set (passthrough labels from other integrations) are untouched.
 *
 * Note: paths use simple `/{key}` format — the backend's normalizePath strips the
 * leading `/` and uses the remainder as a flat map key (no RFC 6901 unescaping).
 */
export function diffLabels(
  originalEntries: [string, string][],
  newLabels: Record<string, string>,
): PatchOp[] {
  const ops: PatchOp[] = [];
  const originalMap = Object.fromEntries(originalEntries);

  for (const [key, value] of Object.entries(newLabels)) {
    if (!(key in originalMap)) {
      ops.push({ op: "add", path: `/${key}`, value });
    } else if (originalMap[key] !== value) {
      ops.push({ op: "replace", path: `/${key}`, value });
    }
  }

  for (const [key] of originalEntries) {
    if (!(key in newLabels)) {
      ops.push({ op: "remove", path: `/${key}` });
    }
  }

  return ops;
}
