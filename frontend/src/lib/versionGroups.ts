import type { VersionedListItem } from "@/api/types";
import { stackNamespaceLabel } from "@/lib/parseStackLabels";

/**
 * What arranging reads of a config or secret list item.
 */
export interface Groupable extends VersionedListItem {
  ID: string;
  CreatedAt: string;
  Spec: { Labels: Record<string, string> | null };
}

/**
 * Where a row sits among its versions. `olderVersions` is set on the newest
 * version alone; `olderVersion` marks one shown beneath it.
 */
export interface VersionPlacement {
  groupKey?: string | undefined;
  olderVersions?: number | undefined;
  olderVersion?: boolean | undefined;
}

function keyOf({ VersionOf, Spec: { Labels } }: Groupable): string | undefined {
  if (!VersionOf) {
    return undefined;
  }

  return `${Labels?.[stackNamespaceLabel] ?? ""}\u0000${VersionOf}`;
}

function newestFirst(left: Groupable, right: Groupable): number {
  const byCreation = Date.parse(right.CreatedAt) - Date.parse(left.CreatedAt);

  if (byCreation !== 0) {
    return byCreation;
  }

  return right.ID < left.ID ? -1 : right.ID > left.ID ? 1 : 0;
}

/**
 * Collapses each group of versions the server named into its newest version,
 * at the place the group first appears, followed by the older ones when the
 * group is expanded. Only the loaded items are grouped.
 */
export function arrangeVersions<T extends Groupable>(
  items: T[],
  expanded: ReadonlySet<string>,
): Array<T & VersionPlacement> {
  const groups = new Map<string, T[]>();

  for (const item of items) {
    const key = keyOf(item);

    if (key !== undefined) {
      groups.set(key, [...(groups.get(key) ?? []), item]);
    }
  }

  const rows: Array<T & VersionPlacement> = [];
  const placed = new Set<string>();

  for (const item of items) {
    const key = keyOf(item);

    if (key === undefined) {
      rows.push({ ...item, olderVersions: 0, olderVersion: false });
      continue;
    }

    if (placed.has(key)) {
      continue;
    }

    placed.add(key);

    const [newest, ...older] = [...(groups.get(key) ?? [])].sort(newestFirst);

    if (!newest) {
      continue;
    }

    rows.push({ ...newest, groupKey: key, olderVersions: older.length, olderVersion: false });

    if (expanded.has(key)) {
      for (const version of older) {
        rows.push({ ...version, groupKey: key, olderVersions: 0, olderVersion: true });
      }
    }
  }

  return rows;
}
