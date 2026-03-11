import { useState } from "react";
import { X, BarChart3 } from "lucide-react";

const DISMISS_KEY = "cetacean:dismiss-prom-banner";

export default function PrometheusBanner({ configured }: { configured: boolean }) {
  const [dismissed, setDismissed] = useState(() => localStorage.getItem(DISMISS_KEY) === "true");

  if (configured || dismissed) return null;

  return (
    <div className="flex items-center gap-3 rounded-lg border border-blue-500/30 bg-blue-500/10 px-4 py-3 mb-4">
      <BarChart3 className="size-5 text-blue-400 shrink-0" />
      <p className="text-sm text-blue-200 flex-1">
        Set{" "}
        <code className="rounded bg-blue-500/20 px-1.5 py-0.5 text-xs font-mono">
          CETACEAN_PROMETHEUS_URL
        </code>{" "}
        to enable CPU, memory, and disk utilization metrics.
      </p>
      <button
        onClick={() => {
          localStorage.setItem(DISMISS_KEY, "true");
          setDismissed(true);
        }}
        className="text-blue-400/60 hover:text-blue-300 transition-colors"
        aria-label="Dismiss"
      >
        <X className="size-4" />
      </button>
    </div>
  );
}
