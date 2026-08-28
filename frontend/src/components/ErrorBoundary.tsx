import { AlertTriangle } from "lucide-react";
import type { ErrorInfo, ReactNode } from "react";
import { Component } from "react";

interface Props {
  children: ReactNode;
  /** When true, renders a compact inline fallback instead of the full-page one. */
  inline?: boolean | undefined;
}

interface State {
  error: Error | null;
}

export default class ErrorBoundary extends Component<Props, State> {
  override state: State = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo) {
    // eslint-disable-next-line no-console
    console.error("ErrorBoundary caught:", error, info.componentStack);
  }

  override render() {
    if (this.state.error) {
      if (this.props.inline) {
        return (
          <div className="flex items-center gap-2 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-400">
            <AlertTriangle className="size-4 shrink-0" />
            <span className="truncate">{this.state.error.message}</span>
            <button
              onClick={() => this.setState({ error: null })}
              className="ms-auto shrink-0 text-xs font-medium underline hover:no-underline"
            >
              Retry
            </button>
          </div>
        );
      }

      return (
        <div className="flex flex-col items-center justify-center py-20 text-center">
          <AlertTriangle className="mb-4 size-12 text-red-500" />
          <h2 className="mb-2 text-lg font-semibold">Something went wrong</h2>
          <p className="mb-4 max-w-md text-sm text-muted-foreground">
            This view couldn&apos;t be displayed. The cluster data may have changed unexpectedly
            while the page was open. Reloading usually resolves it.
          </p>
          <div className="mb-4 flex gap-2">
            <button
              onClick={() => this.setState({ error: null })}
              className="rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted"
            >
              Try again
            </button>
            <button
              onClick={() => window.location.reload()}
              className="rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted"
            >
              Reload page
            </button>
          </div>
          <details className="max-w-md text-left text-xs text-muted-foreground">
            <summary className="cursor-pointer select-none hover:text-foreground">
              Technical details
            </summary>
            <pre className="mt-2 overflow-x-auto rounded-md border bg-muted/30 p-3 font-mono break-words whitespace-pre-wrap">
              {this.state.error.message}
            </pre>
          </details>
        </div>
      );
    }

    return this.props.children;
  }
}
