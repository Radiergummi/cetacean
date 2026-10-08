import { apiPath } from "@/lib/basePath";
import { useQuery } from "@tanstack/react-query";

/** The watcher block of GET /-/health. Absent when no watcher is attached. */
interface WatcherHealth {
  connected: boolean;
  lastSyncAt?: string;
  lastSyncAgeSeconds?: number;
}

export interface ClusterFreshness {
  /** Whether Cetacean is still tracking the cluster. */
  tracking: boolean;

  lastSyncAgeSeconds: number | undefined;
}

const clusterFreshnessQueryKey = ["cluster-freshness"] as const;

/**
 * Polls the health endpoint for whether Cetacean is still tracking the cluster.
 * Losing the Docker connection does not disconnect the browser, so nothing else
 * distinguishes a live dashboard from a frozen one.
 *
 * A failed or unrecognised response reports `tracking: true`: warn about known
 * staleness, not about a failed poll.
 */
export function useClusterFreshness(): ClusterFreshness {
  const { data } = useQuery({
    queryKey: clusterFreshnessQueryKey,
    queryFn: async (): Promise<WatcherHealth | null> => {
      const response = await fetch(apiPath("/-/health"), {
        headers: { Accept: "application/json" },
      });

      if (!response.ok) {
        return null;
      }

      const body = (await response.json()) as { watcher?: WatcherHealth };

      return body.watcher ?? null;
    },
    refetchInterval: 30_000,
    staleTime: 15_000,
  });

  if (!data) {
    return { tracking: true, lastSyncAgeSeconds: undefined };
  }

  return {
    tracking: data.connected,
    lastSyncAgeSeconds: data.lastSyncAgeSeconds,
  };
}

/** Renders a sync age the way the rest of the indicator renders time. */
export function formatSyncAge(seconds: number | undefined): string {
  if (seconds === undefined) {
    return "";
  }

  if (seconds < 60) {
    return `${Math.round(seconds)}s ago`;
  }

  if (seconds < 3_600) {
    return `${Math.floor(seconds / 60)}m ago`;
  }

  return `${Math.floor(seconds / 3_600)}h ago`;
}
