import { api } from "@/api/client";
import { useCallback, useEffect, useRef, useState } from "react";

export interface Suggestion {
  label: string;
  type: "metric" | "function" | "label" | "value";
  detail?: string;
}

interface QueryCompletion {
  suggestions: Suggestion[];
  loading: boolean;
  complete: (query: string, cursorPosition: number) => void;
  clear: () => void;
}

/**
 * Static list of PromQL functions and aggregation operators.
 */
const promqlFunctions: Suggestion[] = [
  { label: "abs", type: "function", detail: "abs(v instant-vector)" },
  { label: "absent", type: "function", detail: "absent(v instant-vector)" },
  { label: "avg_over_time", type: "function", detail: "avg_over_time(v range-vector)" },
  { label: "ceil", type: "function", detail: "ceil(v instant-vector)" },
  { label: "changes", type: "function", detail: "changes(v range-vector)" },
  { label: "clamp", type: "function", detail: "clamp(v instant-vector, min, max scalar)" },
  { label: "count_over_time", type: "function", detail: "count_over_time(v range-vector)" },
  { label: "delta", type: "function", detail: "delta(v range-vector)" },
  { label: "deriv", type: "function", detail: "deriv(v range-vector)" },
  { label: "exp", type: "function", detail: "exp(v instant-vector)" },
  { label: "floor", type: "function", detail: "floor(v instant-vector)" },
  {
    label: "histogram_quantile",
    type: "function",
    detail: "histogram_quantile(phi scalar, b instant-vector)",
  },
  { label: "idelta", type: "function", detail: "idelta(v range-vector)" },
  { label: "increase", type: "function", detail: "increase(v range-vector)" },
  { label: "irate", type: "function", detail: "irate(v range-vector)" },
  {
    label: "label_join",
    type: "function",
    detail: "label_join(v instant-vector, dst, sep, ...src)",
  },
  {
    label: "label_replace",
    type: "function",
    detail: "label_replace(v instant-vector, dst, replacement, src, regex)",
  },
  { label: "ln", type: "function", detail: "ln(v instant-vector)" },
  { label: "log2", type: "function", detail: "log2(v instant-vector)" },
  { label: "log10", type: "function", detail: "log10(v instant-vector)" },
  { label: "max_over_time", type: "function", detail: "max_over_time(v range-vector)" },
  { label: "min_over_time", type: "function", detail: "min_over_time(v range-vector)" },
  {
    label: "predict_linear",
    type: "function",
    detail: "predict_linear(v range-vector, t scalar)",
  },
  { label: "rate", type: "function", detail: "rate(v range-vector)" },
  { label: "resets", type: "function", detail: "resets(v range-vector)" },
  { label: "round", type: "function", detail: "round(v instant-vector, to_nearest scalar)" },
  { label: "scalar", type: "function", detail: "scalar(v instant-vector)" },
  { label: "sort", type: "function", detail: "sort(v instant-vector)" },
  { label: "sort_desc", type: "function", detail: "sort_desc(v instant-vector)" },
  { label: "sqrt", type: "function", detail: "sqrt(v instant-vector)" },
  { label: "stddev_over_time", type: "function", detail: "stddev_over_time(v range-vector)" },
  { label: "sum_over_time", type: "function", detail: "sum_over_time(v range-vector)" },
  { label: "time", type: "function", detail: "time()" },
  { label: "timestamp", type: "function", detail: "timestamp(v instant-vector)" },
  { label: "vector", type: "function", detail: "vector(s scalar)" },
  { label: "year", type: "function", detail: "year(v=vector(time()) instant-vector)" },
  { label: "month", type: "function", detail: "month(v=vector(time()) instant-vector)" },
  {
    label: "day_of_month",
    type: "function",
    detail: "day_of_month(v=vector(time()) instant-vector)",
  },
  {
    label: "day_of_week",
    type: "function",
    detail: "day_of_week(v=vector(time()) instant-vector)",
  },
  {
    label: "day_of_year",
    type: "function",
    detail: "day_of_year(v=vector(time()) instant-vector)",
  },
  {
    label: "days_in_month",
    type: "function",
    detail: "days_in_month(v=vector(time()) instant-vector)",
  },
  { label: "hour", type: "function", detail: "hour(v=vector(time()) instant-vector)" },
  { label: "minute", type: "function", detail: "minute(v=vector(time()) instant-vector)" },
  { label: "avg", type: "function", detail: "avg(v instant-vector) [aggregation]" },
  { label: "count", type: "function", detail: "count(v instant-vector) [aggregation]" },
  { label: "group", type: "function", detail: "group(v instant-vector) [aggregation]" },
  { label: "max", type: "function", detail: "max(v instant-vector) [aggregation]" },
  { label: "min", type: "function", detail: "min(v instant-vector) [aggregation]" },
  { label: "stddev", type: "function", detail: "stddev(v instant-vector) [aggregation]" },
  { label: "stdvar", type: "function", detail: "stdvar(v instant-vector) [aggregation]" },
  { label: "sum", type: "function", detail: "sum(v instant-vector) [aggregation]" },
  { label: "topk", type: "function", detail: "topk(k scalar, v instant-vector) [aggregation]" },
  {
    label: "bottomk",
    type: "function",
    detail: "bottomk(k scalar, v instant-vector) [aggregation]",
  },
  {
    label: "count_values",
    type: "function",
    detail: 'count_values("label", v instant-vector) [aggregation]',
  },
  {
    label: "quantile",
    type: "function",
    detail: "quantile(phi scalar, v instant-vector) [aggregation]",
  },
];

const minimumPrefixLength = 2;
const maxSuggestions = 20;

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
 * Checks if the cursor is inside a label matcher block ({...}).
 */
function isInsideBraces(query: string, cursor: number): boolean {
  let depth = 0;

  for (let i = 0; i < cursor; i++) {
    if (query[i] === "{") {
      depth++;
    } else if (query[i] === "}") {
      depth--;
    }
  }

  return depth > 0;
}

/**
 * Provides PromQL autocompletion for metric names and functions.
 * Metric names are fetched once from Prometheus on first use and cached.
 */
export function useQueryCompletion(enabled: boolean): QueryCompletion {
  const [suggestions, setSuggestions] = useState<Suggestion[]>([]);
  const [loading, setLoading] = useState(false);
  const allSuggestionsRef = useRef<Suggestion[] | null>(null);
  const fetchingRef = useRef(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => {
    return () => clearTimeout(debounceRef.current);
  }, []);

  const doComplete = useCallback((query: string, cursorPosition: number, all: Suggestion[]) => {
    if (isInsideBraces(query, cursorPosition)) {
      setSuggestions([]);
      return;
    }

    const { start } = getTokenBounds(query, cursorPosition);
    const prefix = query.slice(start, cursorPosition);

    if (prefix.length < minimumPrefixLength) {
      setSuggestions([]);
      return;
    }

    const lowerPrefix = prefix.toLowerCase();
    const matches: Suggestion[] = [];

    for (const suggestion of all) {
      if (suggestion.label.toLowerCase().startsWith(lowerPrefix)) {
        matches.push(suggestion);
      }

      if (matches.length >= maxSuggestions) {
        break;
      }
    }

    setSuggestions(matches);
  }, []);

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
