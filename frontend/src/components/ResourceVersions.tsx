import type { VersionedListItem } from "@/api/types";
import ResourceName from "@/components/ResourceName";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { VersionPlacement } from "@/lib/versionGroups";
import { useCallback, useState } from "react";

type Row = VersionedListItem &
  VersionPlacement & {
    ID: string;
    Spec: { Name: string; Labels: Record<string, string> | null };
  };

/**
 * Which version groups are expanded, and a toggle for one.
 */
export function useExpandedVersions() {
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());

  const toggle = useCallback((key: string) => {
    setExpanded((current) => {
      const next = new Set(current);

      if (!next.delete(key)) {
        next.add(key);
      }

      return next;
    });
  }, []);

  return { expanded, toggle };
}

/**
 * A config or secret name with its place among its versions: the newest
 * carries a toggle for the older ones, which render indented beneath it.
 */
export function VersionedName({
  item: { ID, Spec, groupKey, olderVersions, olderVersion, VersionInferred },
  expanded,
  onToggle,
}: {
  item: Row;
  expanded: ReadonlySet<string>;
  onToggle: (key: string) => void;
}) {
  const name = <ResourceName name={Spec.Name || ID} />;

  if (olderVersion) {
    return <div className="border-l-2 pl-3">{name}</div>;
  }

  if (!groupKey || !olderVersions) {
    return name;
  }

  const open = expanded.has(groupKey);

  return (
    <div className="flex items-center gap-2">
      {name}
      <button
        type="button"
        aria-expanded={open}
        onClick={(event) => {
          // The row navigates on click, and a card is a link.
          event.preventDefault();
          event.stopPropagation();
          onToggle(groupKey);
        }}
        className="rounded-md border px-1.5 text-xs text-muted-foreground hover:bg-muted"
      >
        {open ? "Hide older" : `+${olderVersions} older`}
      </button>
      {VersionInferred && (
        <Tooltip>
          <TooltipTrigger
            render={<span className="text-xs text-muted-foreground">inferred from names</span>}
          />
          <TooltipContent>
            Grouped because the names differ only in a version suffix. Label each version
            cetacean.version-of to group them for certain.
          </TooltipContent>
        </Tooltip>
      )}
    </div>
  );
}

/**
 * How many services use a config or secret; an older version nothing uses
 * reads as unused.
 */
export function UsedBy({ item: { UsedBy, olderVersion } }: { item: Row }) {
  if (olderVersion && UsedBy === 0) {
    return <span className="text-muted-foreground">Unused</span>;
  }

  return <span className="tabular-nums">{UsedBy}</span>;
}
