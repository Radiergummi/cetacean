import { rangeSeconds } from "./useMetricsSeries";
import { api } from "@/api/client";
import { useDebouncedInvalidation } from "@/hooks/useDebouncedInvalidation";
import { type TimelineItem, timelineItems } from "@/lib/timeline";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

/**
 * The resource whose history a panel's charts mark.
 */
export interface TimelineSource {
  type: "service" | "node" | "task";
  resourceId: string;
}

const noItems: TimelineItem[] = [];

function isoSeconds(seconds: number): string {
  return new Date(seconds * 1000).toISOString();
}

/**
 * A resource's changes and gaps inside the panel's window. Its stream's
 * changes refetch it, and so does a sync, which is how a new gap arrives.
 */
export function useTimeline(
  source: TimelineSource | undefined,
  timeWindow: {
    range: string;
    from?: number | undefined;
    to?: number | undefined;
    refreshKey: number;
  },
): { items: TimelineItem[]; truncated: boolean } {
  const { range, from, to, refreshKey } = timeWindow;
  const type = source?.type;
  const resourceId = source?.resourceId;
  const queryKey = useMemo(
    () => ["timeline", type, resourceId, range, from, to, refreshKey] as const,
    [type, resourceId, range, from, to, refreshKey],
  );

  const { data } = useQuery({
    queryKey,
    queryFn: ({ signal }) => {
      if (type === undefined || resourceId === undefined) {
        return { entries: [], truncated: false };
      }

      const since = from ?? Math.floor(Date.now() / 1000) - rangeSeconds(range);

      return api.timeline(
        {
          type,
          resourceId,
          since: isoSeconds(since),
          until: to === undefined ? undefined : isoSeconds(to),
        },
        signal,
      );
    },
    enabled: type !== undefined && resourceId !== undefined,
    // A new window or a refresh keeps the markers up until it loads; another
    // resource's markers would be wrong, so those are not kept.
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[1] === type && previousQuery?.queryKey[2] === resourceId
        ? previous
        : undefined,
  });

  useDebouncedInvalidation(
    type && resourceId ? `/${type}s/${resourceId}` : undefined,
    [queryKey],
    500,
    undefined,
    (event) => event.type === type,
  );

  const items = useMemo(
    () => (data && type ? timelineItems(data.entries, type) : noItems),
    [data, type],
  );

  return { items, truncated: data?.truncated ?? false };
}
