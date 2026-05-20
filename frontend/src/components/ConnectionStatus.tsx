import { api } from "../api/client";
import { useConnection } from "../hooks/useResourceStream";
import { Spinner } from "./Spinner";
import { Button } from "./ui/button";
import { RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";

export default function ConnectionStatus() {
  const { connected, lastEventAt } = useConnection();
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
      title={connected ? `Connected${ago ? ` · last event ${ago}` : ""}` : "Reconnecting…"}
    >
      <div
        data-connected={connected || undefined}
        data-pulsing={pulsing || undefined}
        className="size-2 animate-pulse rounded-full bg-red-500 transition-shadow duration-300 data-connected:animate-none data-connected:bg-green-500 data-pulsing:shadow-[0_0_6px_2px_rgba(34,197,94,0.5)]"
      />
      <span className="hidden text-xs text-muted-foreground sm:inline">
        {connected ? (
          <>
            <span>Live</span>
            {ago ? (
              <span className="hidden min-w-16 tabular-nums xl:inline"> · {ago}</span>
            ) : undefined}
          </>
        ) : (
          "Reconnecting"
        )}
      </span>
      <Button
        variant="ghost"
        size="icon-xs"
        onClick={() => void handleResync()}
        disabled={resyncing}
        title="Force a full re-sync from Docker. Use when the dashboard appears to be showing stale state."
        aria-label="Resync from Docker"
      >
        {resyncing ? <Spinner className="size-3" /> : <RefreshCw className="size-3" />}
      </Button>
    </div>
  );
}
