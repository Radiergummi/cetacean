import { api } from "../api/client";
import { formatSyncAge, useClusterFreshness } from "../hooks/useClusterFreshness";
import { useConnection } from "../hooks/useResourceStream";
import { Spinner } from "./Spinner";
import { Button } from "./ui/button";
import { RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";

/** The browser has lost Cetacean, Cetacean has lost Docker, or all is current. */
function statusLabel({ connected, stale }: { connected: boolean; stale: boolean }): string {
  if (!connected) {
    return "Reconnecting";
  }

  if (stale) {
    return "Stale";
  }

  return "Live";
}

function statusTitle({
  connected,
  stale,
  ago,
  syncAge,
}: {
  connected: boolean;
  stale: boolean;
  ago: string;
  syncAge: string;
}): string {
  if (!connected) {
    return "Reconnecting…";
  }

  if (stale) {
    return (
      "Cetacean cannot reach Docker. Everything shown is the state it last read" +
      (syncAge ? `, ${syncAge}` : "") +
      "; it will catch up on its own once the connection returns."
    );
  }

  return `Connected${ago ? ` · last event ${ago}` : ""}`;
}

export default function ConnectionStatus() {
  const { connected, lastEventAt } = useConnection();
  const { tracking, lastSyncAgeSeconds } = useClusterFreshness();

  // "Live" over frozen data is the one thing this indicator must not show.
  const stale = connected && !tracking;
  const syncAge = formatSyncAge(lastSyncAgeSeconds);
  const [ago, setAgo] = useState("");
  const [pulsing, setPulsing] = useState(false);
  const [resyncing, setResyncing] = useState(false);
  const previousEventRef = useRef(lastEventAt);

  async function handleResync() {
    if (resyncing) {
      return;
    }

    setResyncing(true);
    try {
      await api.resync();
    } catch (error) {
      console.warn("resync failed", error);
    } finally {
      setResyncing(false);
    }
  }

  // Brief pulse when a new event arrives
  useEffect(() => {
    if (lastEventAt && lastEventAt !== previousEventRef.current) {
      previousEventRef.current = lastEventAt;
      setPulsing(true);
      const timeout = setTimeout(() => setPulsing(false), 600);

      return () => clearTimeout(timeout);
    }

    return undefined;
  }, [lastEventAt]);

  // Update relative time every second
  useEffect(() => {
    if (!lastEventAt) {
      return;
    }

    const update = () => {
      const seconds = Math.round((Date.now() - lastEventAt) / 1_000);

      if (seconds < 5) {
        setAgo("just now");
      } else if (seconds < 60) {
        setAgo(`${seconds}s ago`);
      } else {
        setAgo(`${Math.floor(seconds / 60)}m ago`);
      }
    };

    update();

    const interval = setInterval(update, 1_000);

    return () => clearInterval(interval);
  }, [lastEventAt]);

  return (
    <div
      className="flex items-center gap-1.5"
      role="status"
      aria-live="polite"
      title={statusTitle({ connected, stale, ago, syncAge })}
    >
      <div
        data-connected={(connected && !stale) || undefined}
        data-stale={stale || undefined}
        data-pulsing={pulsing || undefined}
        className="size-2 animate-pulse rounded-full bg-status-danger transition-shadow duration-300 data-connected:animate-none data-connected:bg-status-ok data-pulsing:shadow-[0_0_6px_2px_oklch(from_var(--status-ok)_l_c_h/50%)] data-stale:animate-none data-stale:bg-status-warning"
      />
      <span className="hidden text-xs text-muted-foreground sm:inline">
        {statusLabel({ connected, stale })}
        {connected && !stale && ago ? (
          <span className="hidden min-w-16 tabular-nums xl:inline"> · {ago}</span>
        ) : undefined}
        {stale && syncAge ? (
          <span className="hidden min-w-16 tabular-nums xl:inline"> · {syncAge}</span>
        ) : undefined}
      </span>
      <Button
        variant="ghost"
        size="icon-xs"
        onClick={() => {
          void handleResync();
        }}
        disabled={resyncing}
        title="Force a full re-sync from Docker. Use when the dashboard appears to be showing stale state."
        aria-label="Resync from Docker"
      >
        {resyncing ? <Spinner className="size-3" /> : <RefreshCw className="size-3" />}
      </Button>
    </div>
  );
}
