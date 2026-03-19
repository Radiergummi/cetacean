import { api } from "@/api/client";
import { useCallback, useEffect, useRef, useState } from "react";

export interface Suggestion {
  label: string;
  type: "metric" | "function" | "label" | "value";
  detail?: string;
}

export interface QueryCompletion {
  suggestions: Suggestion[];
  loading: boolean;
  complete: (query: string, cursorPosition: number) => void;
  clear: () => void;
}

function fn(label: string, detail: string): Suggestion {
  return { label, type: "function", detail };
}

/**
 * Static list of PromQL functions and aggregation operators.
 */
const promqlFunctions: Suggestion[] = [
  fn("abs", "abs(v instant-vector)"),
  fn("absent", "absent(v instant-vector)"),
  fn("avg_over_time", "avg_over_time(v range-vector)"),
  fn("ceil", "ceil(v instant-vector)"),
  fn("changes", "changes(v range-vector)"),
  fn("clamp", "clamp(v instant-vector, min, max scalar)"),
  fn("count_over_time", "count_over_time(v range-vector)"),
  fn("delta", "delta(v range-vector)"),
  fn("deriv", "deriv(v range-vector)"),
  fn("exp", "exp(v instant-vector)"),
  fn("floor", "floor(v instant-vector)"),
  fn("histogram_quantile", "histogram_quantile(phi scalar, b instant-vector)"),
  fn("idelta", "idelta(v range-vector)"),
  fn("increase", "increase(v range-vector)"),
  fn("irate", "irate(v range-vector)"),
  fn("label_join", "label_join(v instant-vector, dst, sep, ...src)"),
  fn("label_replace", "label_replace(v instant-vector, dst, replacement, src, regex)"),
  fn("ln", "ln(v instant-vector)"),
  fn("log2", "log2(v instant-vector)"),
  fn("log10", "log10(v instant-vector)"),
  fn("max_over_time", "max_over_time(v range-vector)"),
  fn("min_over_time", "min_over_time(v range-vector)"),
  fn("predict_linear", "predict_linear(v range-vector, t scalar)"),
  fn("rate", "rate(v range-vector)"),
  fn("resets", "resets(v range-vector)"),
  fn("round", "round(v instant-vector, to_nearest scalar)"),
  fn("scalar", "scalar(v instant-vector)"),
  fn("sort", "sort(v instant-vector)"),
  fn("sort_desc", "sort_desc(v instant-vector)"),
  fn("sqrt", "sqrt(v instant-vector)"),
  fn("stddev_over_time", "stddev_over_time(v range-vector)"),
  fn("sum_over_time", "sum_over_time(v range-vector)"),
  fn("time", "time()"),
  fn("timestamp", "timestamp(v instant-vector)"),
  fn("vector", "vector(s scalar)"),
  fn("year", "year(v instant-vector)"),
  fn("month", "month(v instant-vector)"),
  fn("day_of_month", "day_of_month(v instant-vector)"),
  fn("day_of_week", "day_of_week(v instant-vector)"),
  fn("day_of_year", "day_of_year(v instant-vector)"),
  fn("days_in_month", "days_in_month(v instant-vector)"),
  fn("hour", "hour(v instant-vector)"),
  fn("minute", "minute(v instant-vector)"),
  // Aggregation operators
  fn("avg", "avg(v instant-vector)"),
  fn("count", "count(v instant-vector)"),
  fn("group", "group(v instant-vector)"),
  fn("max", "max(v instant-vector)"),
  fn("min", "min(v instant-vector)"),
  fn("stddev", "stddev(v instant-vector)"),
  fn("stdvar", "stdvar(v instant-vector)"),
  fn("sum", "sum(v instant-vector)"),
  fn("topk", "topk(k scalar, v instant-vector)"),
  fn("bottomk", "bottomk(k scalar, v instant-vector)"),
  fn("count_values", 'count_values("label", v instant-vector)'),
  fn("quantile", "quantile(phi scalar, v instant-vector)"),
];

const minimumPrefixLength = 2;
const maxSuggestions = 20;

export type CursorContext =
  | { type: "metric" }
  | { type: "label"; metricName: string }
  | { type: "value"; metricName: string; labelName: string };

/**
 * Determines what type of completion is needed based on cursor position.
 * Detects whether the cursor is inside PromQL `{}` braces and whether
 * a label name or label value is being typed.
 */
export function getCursorContext(query: string, cursor: number): CursorContext {
  // Find the nearest unmatched { before cursor
  let braceDepth = 0;
  let bracePosition = -1;

  for (let i = cursor - 1; i >= 0; i--) {
    if (query[i] === "}") {
      braceDepth++;
    }

    if (query[i] === "{") {
      if (braceDepth === 0) {
        bracePosition = i;
        break;
      }

      braceDepth--;
    }
  }

  if (bracePosition === -1) {
    return { type: "metric" };
  }

  // Extract metric name before {
  const beforeBrace = query.slice(0, bracePosition);
  const metricMatch = beforeBrace.match(/([a-zA-Z_:][a-zA-Z0-9_:]*)$/);
  const metricName = metricMatch?.[1] ?? "";

  // Scan the content between { and cursor to find context
  const insideBraces = query.slice(bracePosition + 1, cursor);

  // After =" means we're typing a label value
  const valueMatch = insideBraces.match(/(\w+)\s*=~?\s*"[^"]*$/);

  if (valueMatch) {
    return { type: "value", metricName, labelName: valueMatch[1] };
  }

  // Otherwise we're typing a label name
  return { type: "label", metricName };
}

/**
 * Checks if `query` matches `target` using segment-prefix matching.
 * Splits the target by `_` and `-` into segments, then tries to consume all
 * query characters by matching them against prefixes of segments in order.
 * Segments can be skipped; uses backtracking when greedy matching fails.
 *
 * Examples against "go_gc_cleanups_executed_cleanups_total":
 * - "ggclext" matches (g→go, g→gc, cl→cleanups, ex→executed, t→total)
 * - "gotot" matches (go→go, tot→total)
 * - "contcpu" matches "container_cpu_usage_seconds_total"
 */
export function segmentPrefixMatch(target: string, query: string): boolean {
  if (query.length === 0) {
    return true;
  }

  const segments = target.toLowerCase().split(/[_-]/);
  const q = query.toLowerCase().replace(/[_-]/g, "");

  // Single-segment targets are already covered by startsWith in the caller
  if (segments.length <= 1) {
    return false;
  }

  const memo = new Map<number, boolean>();

  function match(queryIndex: number, segmentIndex: number): boolean {
    if (queryIndex >= q.length) {
      return true;
    }

    if (segmentIndex >= segments.length) {
      return false;
    }

    const key = queryIndex * segments.length + segmentIndex;
    const cached = memo.get(key);

    if (cached !== undefined) {
      return cached;
    }

    let result = false;

    for (let s = segmentIndex; s < segments.length && !result; s++) {
      const segment = segments[s];

      // Find how many query chars match this segment's prefix
      let maxMatch = 0;

      while (
        maxMatch < segment.length &&
        queryIndex + maxMatch < q.length &&
        q[queryIndex + maxMatch] === segment[maxMatch]
      ) {
        maxMatch++;
      }

      // Try all valid match lengths (1..maxMatch), not just greedy
      for (let take = maxMatch; take >= 1 && !result; take--) {
        if (match(queryIndex + take, s + 1)) {
          result = true;
        }
      }
    }

    memo.set(key, result);
    return result;
  }

  return match(0, 0);
}

/**
 * Extracts the current token boundaries at the cursor position.
 */
export function getTokenBounds(text: string, cursor: number): { start: number; end: number } {
  let start = cursor;

  while (start > 0 && /[a-zA-Z0-9_:]/.test(text[start - 1])) {
    start--;
  }

  let end = cursor;

  while (end < text.length && /[a-zA-Z0-9_:]/.test(text[end])) {
    end++;
  }

  return { start, end };
}

/**
 * Extracts the prefix being typed inside a quoted label value.
 * Scans backward from cursor to the opening `"`.
 */
function getValuePrefix(query: string, cursor: number): string {
  for (let i = cursor - 1; i >= 0; i--) {
    if (query[i] === '"') {
      return query.slice(i + 1, cursor);
    }
  }

  return "";
}

/**
 * Provides PromQL autocompletion for metric names, functions,
 * label names, and label values.
 * Metric names are fetched once from Prometheus on first use and cached.
 * Label names and values are fetched on demand and cached per key.
 */
export function useQueryCompletion(enabled: boolean): QueryCompletion {
  const [suggestions, setSuggestions] = useState<Suggestion[]>([]);
  const [loading, setLoading] = useState(false);
  const allSuggestionsRef = useRef<Suggestion[] | null>(null);
  const fetchingRef = useRef(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const labelCacheRef = useRef<Map<string, string[]>>(new Map());
  const valueCacheRef = useRef<Map<string, string[]>>(new Map());

  useEffect(() => {
    return () => clearTimeout(debounceRef.current);
  }, []);

  /**
   * Fetches items from cache or API, then filters by prefix and sets suggestions.
   */
  const completeCached = useCallback(
    async (
      cache: Map<string, string[]>,
      cacheKey: string,
      fetcher: () => Promise<string[]>,
      prefix: string,
      suggestionType: Suggestion["type"],
      exclude?: string,
    ) => {
      let items = cache.get(cacheKey);

      if (!items) {
        setLoading(true);

        try {
          items = await fetcher();
          cache.set(cacheKey, items);
        } catch {
          items = [];
        } finally {
          setLoading(false);
        }
      }

      const lowerPrefix = prefix.toLowerCase();
      const prefixMatches: Suggestion[] = [];
      const fuzzyMatches: Suggestion[] = [];

      for (const item of items) {
        if (item === exclude) {
          continue;
        }

        if (lowerPrefix.length === 0 || item.toLowerCase().startsWith(lowerPrefix)) {
          prefixMatches.push({ label: item, type: suggestionType });
        } else if (lowerPrefix.length >= minimumPrefixLength && segmentPrefixMatch(item, lowerPrefix)) {
          fuzzyMatches.push({ label: item, type: suggestionType });
        }
      }

      setSuggestions([...prefixMatches, ...fuzzyMatches].slice(0, maxSuggestions));
    },
    [],
  );

  const doComplete = useCallback(
    (query: string, cursorPosition: number, all: Suggestion[]) => {
      const context = getCursorContext(query, cursorPosition);

      if (context.type === "label") {
        const cacheKey = context.metricName || "__all__";
        const match = context.metricName
          ? `{__name__="${context.metricName}"}`
          : undefined;
        const { start } = getTokenBounds(query, cursorPosition);
        const prefix = query.slice(start, cursorPosition);

        completeCached(
          labelCacheRef.current,
          cacheKey,
          () => api.metricsLabels(match),
          prefix,
          "label",
          "__name__",
        );
        return;
      }

      if (context.type === "value") {
        const prefix = getValuePrefix(query, cursorPosition);

        completeCached(
          valueCacheRef.current,
          context.labelName,
          () => api.metricsLabelValues(context.labelName),
          prefix,
          "value",
        );
        return;
      }

      const { start } = getTokenBounds(query, cursorPosition);
      const prefix = query.slice(start, cursorPosition);

      if (prefix.length < minimumPrefixLength) {
        setSuggestions([]);
        return;
      }

      const lowerPrefix = prefix.toLowerCase();
      const prefixMatches: Suggestion[] = [];
      const fuzzyMatches: Suggestion[] = [];

      for (const suggestion of all) {
        if (suggestion.label.toLowerCase().startsWith(lowerPrefix)) {
          prefixMatches.push(suggestion);
        } else if (segmentPrefixMatch(suggestion.label, lowerPrefix)) {
          fuzzyMatches.push(suggestion);
        }
      }

      setSuggestions([...prefixMatches, ...fuzzyMatches].slice(0, maxSuggestions));
    },
    [completeCached],
  );

  const complete = useCallback(
    (query: string, cursorPosition: number) => {
      if (!enabled) {
        setSuggestions([]);
        return;
      }

      clearTimeout(debounceRef.current);

      debounceRef.current = setTimeout(() => {
        if (allSuggestionsRef.current !== null) {
          doComplete(query, cursorPosition, allSuggestionsRef.current);
          return;
        }

        if (fetchingRef.current) {
          return;
        }

        fetchingRef.current = true;
        setLoading(true);

        api
          .metricsLabelValues("__name__")
          .then((names) => {
            const metrics = names.map((name): Suggestion => ({ label: name, type: "metric" }));
            allSuggestionsRef.current = [...promqlFunctions, ...metrics];
            doComplete(query, cursorPosition, allSuggestionsRef.current);
          })
          .catch(() => {
            allSuggestionsRef.current = [...promqlFunctions];
            doComplete(query, cursorPosition, allSuggestionsRef.current);
          })
          .finally(() => {
            fetchingRef.current = false;
            setLoading(false);
          });
      }, 80);
    },
    [enabled, doComplete],
  );

  const clear = useCallback(() => {
    setSuggestions([]);
  }, []);

  return { suggestions, loading, complete, clear };
}
