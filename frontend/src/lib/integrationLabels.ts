import { api } from "@/api/client";
import type { PatchOp } from "@/api/types";

/**
 * Compute JSON Patch operations from label changes.
 *
 * Compares `newLabels` against the original raw label entries for an integration.
 * Keys present in `originalEntries` but absent from `newLabels` are removed.
 * Keys not in either set (pass-through labels from other integrations) are untouched.
 *
 * Note: paths use a simple `/{key}` format — the backend's normalizePath strips the
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

/**
 * Save integration labels by diffing the new label state against the original
 * raw labels, then patching via the service labels API.
 */
export async function saveIntegrationLabels(
  rawLabels: [string, string][],
  newLabels: Record<string, string>,
  serviceId: string,
  onSaved: (updated: Record<string, string>) => void,
): Promise<void> {
  const ops = diffLabels(rawLabels, newLabels);
  const updated = await api.patchServiceLabels(serviceId, ops);
  onSaved(updated);
}

/** Shared badge class constants for integration panels. */
const badgeBase = "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium";

export const badgeBlue = `${badgeBase} bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300`;
export const badgePurple = `${badgeBase} bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-300`;
export const badgeTeal = `${badgeBase} bg-teal-100 text-teal-800 dark:bg-teal-900/30 dark:text-teal-300`;
