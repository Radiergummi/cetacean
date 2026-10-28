import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";

const maxEpochSeconds = 8.64e12;

/**
 * The time range in the URL: a named `range`, or `from`/`to` in epoch seconds.
 * Every panel on a page reads it from here, so their windows line up.
 */
export function useTimeRange() {
  const [params, setParams] = useSearchParams();
  const range = params.get("range") ?? "1h";
  const fromParam = params.get("from");
  const toParam = params.get("to");
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
            next.delete("range");
          } else {
            next.set("range", value);
          }

          next.delete("from");
          next.delete("to");

          return next;
        },
        { replace: true },
      );
    },
    [setParams],
  );

  const setCustomRange = useCallback(
    (from: number, to: number) => {
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);

          next.set("from", String(Math.floor(from)));
          next.set("to", String(Math.floor(to)));
          next.delete("range");

          return next;
        },
        { replace: true },
      );
    },
    [setParams],
  );

  const clearCustomRange = useCallback(() => {
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);

        next.delete("from");
        next.delete("to");

        return next;
      },
      { replace: true },
    );
  }, [setParams]);

  return { range, customFrom, customTo, setRange, setCustomRange, clearCustomRange };
}
