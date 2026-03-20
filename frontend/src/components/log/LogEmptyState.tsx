import { Spinner } from "../Spinner";
import { AlertTriangle, FileText } from "lucide-react";
import type React from "react";

interface LogEmptyStateProps {
  loading: boolean;
  error: string | null;
  hasLines: boolean;
  hasFiltered: boolean;
  onRetry: () => void;
  className: string;
  style?: React.CSSProperties;
}

export function LogEmptyState({
  loading,
  error,
  hasLines,
  hasFiltered,
  onRetry,
  className,
  style,
}: LogEmptyStateProps) {
  if (loading) {
    return (
      <div className={className} style={style}>
        <div className="flex flex-col items-center gap-3 text-muted-foreground">
          <Spinner className="size-6" />
          <p className="text-sm">Loading logs…</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className={className} style={style}>
        <div className="flex flex-col items-center gap-3 text-center">
          <AlertTriangle className="size-6 text-red-500 dark:text-red-400" />
          <div>
            <p className="mb-1 text-sm font-medium text-red-600 dark:text-red-400">
              Failed to load logs
            </p>
            <p className="mb-3 text-xs text-muted-foreground">{error}</p>
          </div>
          <button
            onClick={onRetry}
            className="rounded-md border px-4 py-1.5 text-sm hover:bg-muted"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (!hasLines) {
    return (
      <div className={className} style={style}>
        <div className="flex flex-col items-center gap-3 text-muted-foreground">
          <FileText className="size-6" />
          <p className="text-sm">No logs yet — the container hasn't produced any output</p>
        </div>
      </div>
    );
  }

  if (!hasFiltered) {
    return (
      <div className={className} style={style}>
        <p className="text-sm text-muted-foreground">No matching log lines</p>
      </div>
    );
  }

  return null;
}
