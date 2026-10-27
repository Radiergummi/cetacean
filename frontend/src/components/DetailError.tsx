import FetchError from "./FetchError";

interface Props {
  error: unknown;
  /** Shown when the error carries no message of its own. */
  fallback: string;
  onRetry: () => void;
}

/**
 * A detail page that failed to load: the server's own explanation, and a retry.
 */
export default function DetailError({ error, fallback, onRetry }: Props) {
  return (
    <FetchError
      message={error instanceof Error && error.message ? error.message : fallback}
      onRetry={onRetry}
    />
  );
}
