import { AlertTriangle, RefreshCw } from "lucide-react";

interface Props {
  message?: string | undefined;
  onRetry?: (() => void) | undefined;
}

export default function FetchError({ message, onRetry }: Props) {
  return (
    <div className="flex items-center gap-3 rounded-lg border border-status-danger/30 bg-status-danger/10 p-4">
      <AlertTriangle className="size-5 shrink-0 text-status-danger" />
      <div className="flex-1 text-sm text-status-danger">{message || "Failed to load data"}</div>

      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="inline-flex items-center gap-1.5 text-sm font-medium text-status-danger hover:text-status-danger/80"
        >
          <RefreshCw className="size-3.5" />
          Retry
        </button>
      )}
    </div>
  );
}
