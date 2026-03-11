import { AlertTriangle, RefreshCw } from "lucide-react";

interface Props {
  message?: string;
  onRetry?: () => void;
}

export default function FetchError({ message, onRetry }: Props) {
  return (
    <div className="rounded-lg border border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950/30 p-4 flex items-center gap-3">
      <AlertTriangle className="size-5 text-red-600 dark:text-red-400 shrink-0" />
      <div className="flex-1 text-sm text-red-800 dark:text-red-200">
        {message || "Failed to load data"}
      </div>
      {onRetry && (
        <button
          onClick={onRetry}
          className="inline-flex items-center gap-1.5 text-sm font-medium text-red-700 dark:text-red-300 hover:text-red-900 dark:hover:text-red-100"
        >
          <RefreshCw className="size-3.5" />
          Retry
        </button>
      )}
    </div>
  );
}
