import type { VersionedListItem } from "@/api/types";
import ResourceName from "@/components/ResourceName";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useQueries, type UseQueryResult } from "@tanstack/react-query";
import { useCallback, useState } from "react";

/**
 * A config or secret list row; `olderVersion` marks one shown beneath the
 * newest version of its group.
 */
export type VersionRow<T> = T & { olderVersion?: boolean | undefined };

type Item = VersionedListItem & {
  ID: string;
  Spec: { Name: string };
};

/**
 * Which groups are expanded, the older versions the server lists for each,
 * and `arrange`, which places those beneath the newest version they belong to.
 */
export function useVersionExpansion<T extends Item>(
  path: string,
  fetchGroup: (id: string, signal: AbortSignal) => Promise<T[]>,
) {
  const [expanded, setExpanded] = useState<readonly string[]>([]);

  const toggle = useCallback((id: string) => {
    setExpanded((current) =>
      current.includes(id) ? current.filter((each) => each !== id) : [...current, id],
    );
  }, []);

  const combine = useCallback(
    (results: UseQueryResult<T[]>[]) =>
      new Map(results.map(({ data }, index) => [expanded[index] ?? "", data ?? []])),
    [expanded],
  );

  const older = useQueries({
    queries: expanded.map((id) => ({
      queryKey: [path, "versionsOf", id],
      queryFn: ({ signal }: { signal: AbortSignal }) => fetchGroup(id, signal),
    })),
    combine,
  });

  const arrange = useCallback(
    (items: T[]): VersionRow<T>[] =>
      items.flatMap((item) => [
        item,
        ...(older.get(item.ID) ?? [])
          .filter(({ ID }) => ID !== item.ID)
          .map((version) => ({ ...version, olderVersion: true })),
      ]),
    [older],
  );

  return { expanded, toggle, arrange };
}

/**
 * Shows or hides a group's older versions. It sits outside any link, since the
 * card it belongs to is one.
 */
export function OlderVersionsToggle({
  item: { ID, OlderVersions },
  expanded,
  onToggle,
}: {
  item: Item;
  expanded: readonly string[];
  onToggle: (id: string) => void;
}) {
  if (!OlderVersions) {
    return null;
  }

  const open = expanded.includes(ID);

  return (
    <button
      type="button"
      aria-expanded={open}
      onClick={(event) => {
        // The table row navigates on click.
        event.stopPropagation();
        onToggle(ID);
      }}
      className="rounded-md border px-1.5 text-xs text-muted-foreground hover:bg-muted"
    >
      {open ? "Hide older" : `+${OlderVersions} older`}
    </button>
  );
}

/**
 * A config or secret name, with a hint when its group was matched by name.
 * An older version renders indented beneath the newest.
 */
export function VersionedName({
  item: { ID, Spec, VersionInferred, NewestVersion, olderVersion },
}: {
  item: VersionRow<Item>;
}) {
  const name = <ResourceName name={Spec.Name || ID} />;

  if (olderVersion) {
    return <div className="border-l-2 pl-3">{name}</div>;
  }

  if (!NewestVersion || !VersionInferred) {
    return name;
  }

  return (
    <span className="flex items-center gap-2">
      {name}
      <Tooltip>
        <TooltipTrigger
          render={<span className="text-xs text-muted-foreground">inferred from names</span>}
        />
        <TooltipContent>
          Grouped because the names differ only in a version suffix. Label each version
          cetacean.version-of to group them for certain.
        </TooltipContent>
      </Tooltip>
    </span>
  );
}

/**
 * How many services use a config or secret; an older version nothing uses
 * reads as unused.
 */
export function UsedBy({ item: { UsedBy, olderVersion } }: { item: VersionRow<Item> }) {
  if (olderVersion && UsedBy === 0) {
    return <span className="text-muted-foreground">Unused</span>;
  }

  return <span className="tabular-nums">{UsedBy}</span>;
}
