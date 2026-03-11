import { useCallback, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import type { TimeRange } from "./log-utils";
import { formatShortDate, LABEL_TO_RANGE_KEY, RANGE_DURATIONS } from "./log-utils";

export function useLogTimeRange() {
  const [params, setParams] = useSearchParams();

  const initialTimeRange = useMemo((): TimeRange => {
    const rangeKey = params.get("logRange");

    if (rangeKey && RANGE_DURATIONS[rangeKey]) {
      const { label, ms: milliseconds } = RANGE_DURATIONS[rangeKey];
      return { since: new Date(Date.now() - milliseconds).toISOString(), label };
    }

    const logSince = params.get("logSince");
    const logUntil = params.get("logUntil");

    if (logSince || logUntil) {
      let label = "Custom";
      if (logSince && logUntil) {
        label = `${formatShortDate(logSince)} – ${formatShortDate(logUntil)}`;
      } else if (logSince) {
        label = `Since ${formatShortDate(logSince)}`;
      } else if (logUntil) {
        label = `Until ${formatShortDate(logUntil)}`;
      }
      return { since: logSince || undefined, until: logUntil || undefined, label };
    }

    return { label: "All" };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const [timeRange, setTimeRange] = useState<TimeRange>(initialTimeRange);

  const updateTimeRange = useCallback(
    (range: TimeRange) => {
      setTimeRange(range);
      setParams(
        (previous) => {
          const updated = new URLSearchParams(previous);
          updated.delete("logRange");
          updated.delete("logSince");
          updated.delete("logUntil");

          const rangeKey = LABEL_TO_RANGE_KEY[range.label];
          if (rangeKey) {
            updated.set("logRange", rangeKey);
          } else if (range.since || range.until) {
            if (range.since) updated.set("logSince", range.since);
            if (range.until) updated.set("logUntil", range.until);
          }
          return updated;
        },
        { replace: true },
      );
    },
    [setParams],
  );

  return { timeRange, updateTimeRange };
}
