import type { VersionedListItem } from "@/api/types";
import ResourceName from "@/components/ResourceName";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useResourceStream } from "@/hooks/useResourceStream";
import { useQueries, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";

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
  Spec: { Name: string };
};

/**
 * Which groups are expanded, the older versions the server lists for each,
 * and `arrange`, which places those beneath the newest version they belong to.
 * The groups refetch on the events the list itself refetches on.
 */
export function useVersionExpansion<T extends Item>(
  path: string,
  streamPath: string,
  fetchGroup: (id: string, signal: AbortSignal) => Promise<T[]>,
) {
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState<readonly string[]>([]);
  const refetchTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => () => clearTimeout(refetchTimer.current), []);

  useResourceStream(
    expanded.length > 0 ? streamPath : undefined,
    useCallback(() => {
      refetchTimer.current ??= setTimeout(() => {
        refetchTimer.current = undefined;
        void queryClient.invalidateQueries({ queryKey: [path, "versionsOf"] });
      }, refetchDelay);
    }, [queryClient, path]),
  );

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

  // A search lists matching older versions itself, so those stay where it put them.
  const arrange = useCallback(
    (items: T[]): VersionRow<T>[] => {
      const listed = new Set(items.map(({ ID }) => ID));

      return items.flatMap((item) => [
        item,
        ...(older.get(item.ID) ?? [])
          .filter(({ ID }) => !listed.has(ID))
          .map((version) => ({ ...version, olderVersion: true })),
      ]);
    },
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
