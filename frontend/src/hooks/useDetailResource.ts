import { api } from "../api/client";
import type { HistoryEntry } from "../api/types";
import { useResourceStream } from "./useResourceStream";
import { useCallback, useEffect, useRef, useState } from "react";

export function useDetailResource<T>(
  key: string | undefined,
  fetchFn: (key: string, signal?: AbortSignal) => Promise<T>,
  ssePath: string,
) {
  const [data, setData] = useState<T | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [error, setError] = useState<Error | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const fetchData = useCallback(() => {
    if (!key) {
      return;
    }

    abortRef.current?.abort();

    const controller = new AbortController();
    abortRef.current = controller;

    setError(null);

    fetchFn(key, controller.signal)
      .then((d) => {
        if (!controller.signal.aborted) {
          setData(d);
        }
      })
      .catch((thrown) => {
        if (!controller.signal.aborted) {
          setError(thrown instanceof Error ? thrown : new Error(String(thrown)));
        }
      });

    api
      .history({ resourceId: key, limit: 10 }, controller.signal)
      .then((entry) => {
        if (!controller.signal.aborted) {
          setHistory(entry);
        }
      })
      .catch(console.warn);
  }, [key, fetchFn]);

  useEffect(() => {
    fetchData();

    return () => {
      abortRef.current?.abort();
    };
  }, [fetchData]);

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const fetchDataRef = useRef(fetchData);
  fetchDataRef.current = fetchData;

  useResourceStream(
    ssePath,
    useCallback(() => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }

      debounceRef.current = setTimeout(() => {
        debounceRef.current = null;
        fetchDataRef.current();
      }, 500);
    }, []),
  );

  useEffect(() => {
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, []);

  return { data, history, error, retry: fetchData };
}
