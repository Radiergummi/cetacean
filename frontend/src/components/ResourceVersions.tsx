import type { VersionedListItem } from "@/api/types";
import ResourceName from "@/components/ResourceName";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { stackNamespaceLabel } from "@/lib/parseStackLabels";
import { useQueries, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

/**
 * How long expanded groups gather stream events before refetching, as a
 * filtered list does.
 */
const refetchDelay = 750;

/**
 * A config or secret list row; `olderVersion` marks one shown beneath the
 * newest version of its group.
 */
export type VersionRow<T> = T & { olderVersion?: boolean | undefined };

type Item = VersionedListItem & {
  ID: string;
  Spec: { Name: string; Labels: Record<string, string> | null };
};

/**
 * An expanded group, which outlives the version heading it: id is the newest
 * version the list last showed for it, and missingSince the fetch count when
 * the list first showed no row of it.
 */
interface Expansion {
  id: string;
  group: string;
  missingSince?: number | undefined;
}

function groupOf({ VersionOf, Spec: { Labels } }: Item): string {
  return `${Labels?.[stackNamespaceLabel] ?? ""}\u0000${VersionOf ?? ""}`;
}

/**
 * Which groups are expanded, the older versions the server lists for each,
 * and `arrange`, which places those beneath the newest version they belong to.
 * `onEvent` refetches the groups on the events the list itself acts on.
 */
export function useVersionExpansion<T extends Item>(
  path: string,
  fetchGroup: (id: string, signal: AbortSignal) => Promise<T[]>,
) {
  const queryClient = useQueryClient();
  const [expansions, setExpansions] = useState<readonly Expansion[]>([]);
  const expanded = useMemo(() => expansions.map(({ id }) => id), [expansions]);
  const refetchTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => () => clearTimeout(refetchTimer.current), []);

  const onEvent = useCallback(() => {
    refetchTimer.current ??= setTimeout(() => {
      refetchTimer.current = undefined;
      void queryClient.invalidateQueries({ queryKey: [path, "versionsOf"] });
    }, refetchDelay);
  }, [queryClient, path]);

  const toggle = useCallback((item: Item) => {
    setExpansions((current) =>
      current.some(({ id }) => id === item.ID)
        ? current.filter(({ id }) => id !== item.ID)
        : [...current, { id: item.ID, group: groupOf(item) }],
    );
  }, []);

  // A group stays open under whichever version now heads it. It closes only
  // once a fetch has finished with no row of it loaded: a row a stream event
  // took out locally may be replaced by the next version when the list refetches.
  const follow = useCallback((items: T[], fetches: number) => {
    const groups = new Map<string, string | undefined>();

    for (const item of items) {
      if (item.VersionOf !== undefined) {
        const group = groupOf(item);

        groups.set(group, item.NewestVersion ? item.ID : groups.get(group));
      }
    }

    setExpansions((current) => {
      const next = current.flatMap((expansion): Expansion[] => {
        if (groups.has(expansion.group)) {
          return [{ id: groups.get(expansion.group) ?? expansion.id, group: expansion.group }];
        }

        if (expansion.missingSince === undefined) {
          return [{ ...expansion, missingSince: fetches }];
        }

        return fetches > expansion.missingSince ? [] : [expansion];
      });

      const unchanged =
        next.length === current.length &&
        next.every(
          ({ id, missingSince }, index) =>
            id === current[index]?.id && missingSince === current[index]?.missingSince,
        );

      return unchanged ? current : next;
    });
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

  // A search lists matching older versions itself, so those stay where it put them.
  const arrange = useCallback(
    (items: T[]): VersionRow<T>[] => {
      const listed = new Set(items.map(({ ID }) => ID));

      return items.flatMap((item) => [
        item,
        ...(older.get(item.ID) ?? [])
          // A version the list heads elsewhere, or one that overtook a head
          // it has not caught up with, is no older version of this one.
          .filter(({ ID, NewestVersion }) => !listed.has(ID) && !NewestVersion)
          .map((version) => ({ ...version, olderVersion: true })),
      ]);
    },
    [older],
  );

  return { expanded, toggle, arrange, follow, onEvent };
}

/**
 * Shows or hides a group's older versions. It sits outside any link, since the
 * card it belongs to is one.
 */
export function OlderVersionsToggle({
  item,
  expanded,
  onToggle,
}: {
  item: Item;
  expanded: readonly string[];
  onToggle: (item: Item) => void;
}) {
  const { ID, OlderVersions } = item;

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
        onToggle(item);
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
  item: { ID, Spec, VersionInferred, NewestVersion, InUse, olderVersion },
}: {
  item: VersionRow<Item>;
}) {
  const name = <ResourceName name={Spec.Name || ID} />;

  if (olderVersion) {
    return <div className="border-l-2 pl-3">{name}</div>;
  }

  // An older version a service still runs is listed on its own row.
  if (InUse) {
    return (
      <span className="flex items-center gap-2">
        {name}
        <span className="text-xs text-muted-foreground">older, in use</span>
      </span>
    );
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
 * How many services use a config or secret. The count covers only services
 * the person may read, so an older version none of those use says exactly that.
 */
export function UsedBy({
  item: { UsedBy, olderVersion, VersionOf, NewestVersion },
}: {
  item: VersionRow<Item>;
}) {
  const older = olderVersion || (VersionOf !== undefined && !NewestVersion);

  if (older && UsedBy === 0) {
    return (
      <Tooltip>
        <TooltipTrigger
          render={<span className="text-muted-foreground">No services you can see</span>}
        />
        <TooltipContent>
          Counts only the services you may read; one you cannot see may still use it.
        </TooltipContent>
      </Tooltip>
    );
  }

  return <span className="tabular-nums">{UsedBy}</span>;
}
