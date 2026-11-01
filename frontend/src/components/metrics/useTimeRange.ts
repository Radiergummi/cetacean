import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";

const maxEpochSeconds = 8.64e12;

/**
 * The time range in the URL: a named `range`, or `from`/`to` in epoch seconds.
 * A `scope` keeps a section's range apart: "task" reads `taskRange` and so on.
 */
export function useTimeRange(scope?: string | undefined) {
  const [params, setParams] = useSearchParams();
  const rangeKey = scope ? `${scope}Range` : "range";
  const fromKey = scope ? `${scope}From` : "from";
  const toKey = scope ? `${scope}To` : "to";
  const range = params.get(rangeKey) ?? "1h";
  const fromParam = params.get(fromKey);
  const toParam = params.get(toKey);
  // A window that is not one — unparseable, empty, reversed, or past what a
  // Date holds — is no custom range at all, rather than a crash downstream.
  const from = fromParam ? Number(fromParam) : Number.NaN;
  const to = toParam ? Number(toParam) : Number.NaN;
  const valid = from >= 0 && to > from && to <= maxEpochSeconds;
  const customFrom = valid ? from : null;
  const customTo = valid ? to : null;

  const setRange = useCallback(
    (value: string) => {
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);

          if (value === "1h") {
            next.delete(rangeKey);
          } else {
            next.set(rangeKey, value);
          }

          next.delete(fromKey);
          next.delete(toKey);

          return next;
        },
        { replace: true },
      );
    },
    [setParams, rangeKey, fromKey, toKey],
  );

  const setCustomRange = useCallback(
    (from: number, to: number) => {
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);

          next.set(fromKey, String(Math.floor(from)));
          next.set(toKey, String(Math.floor(to)));
          next.delete(rangeKey);

          return next;
        },
        { replace: true },
      );
    },
    [setParams, rangeKey, fromKey, toKey],
  );

  const clearCustomRange = useCallback(() => {
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);

        next.delete(fromKey);
        next.delete(toKey);

        return next;
      },
      { replace: true },
    );
  }, [setParams, fromKey, toKey]);

  return { range, customFrom, customTo, setRange, setCustomRange, clearCustomRange };
}
