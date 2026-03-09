import { useState, useEffect, useRef } from "react";
import { useSSEConnection } from "../hooks/SSEContext";

export default function ConnectionStatus() {
  const { connected, lastEventAt } = useSSEConnection();
  const [ago, setAgo] = useState("");
  const [pulsing, setPulsing] = useState(false);
  const prevEventRef = useRef(lastEventAt);

  // Brief pulse when a new event arrives
  useEffect(() => {
    if (lastEventAt && lastEventAt !== prevEventRef.current) {
      prevEventRef.current = lastEventAt;
      setPulsing(true);
      const t = setTimeout(() => setPulsing(false), 600);
      return () => clearTimeout(t);
    }
  }, [lastEventAt]);

  // Update relative time every second
  useEffect(() => {
    if (!lastEventAt) return;
    const update = () => {
      const seconds = Math.round((Date.now() - lastEventAt) / 1000);
      if (seconds < 5) setAgo("just now");
      else if (seconds < 60) setAgo(`${seconds}s ago`);
      else setAgo(`${Math.floor(seconds / 60)}m ago`);
    };
    update();
    const i = setInterval(update, 1000);
    return () => clearInterval(i);
  }, [lastEventAt]);

  return (
    <div className="flex items-center gap-1.5" title={connected ? `Connected${ago ? ` · last event ${ago}` : ""}` : "Reconnecting..."}>
      <div
        className={`w-2 h-2 rounded-full transition-shadow duration-300 ${
          connected
            ? pulsing
              ? "bg-green-500 shadow-[0_0_6px_2px_rgba(34,197,94,0.5)]"
              : "bg-green-500"
            : "bg-red-500 animate-pulse"
        }`}
      />
      <span className="text-xs text-muted-foreground hidden sm:inline">
        {connected ? (ago ? `Live · ${ago}` : "Live") : "Reconnecting"}
      </span>
    </div>
  );
}
