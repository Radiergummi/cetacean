import { useState, useEffect, useCallback, useRef } from "react";
import { api } from "../api/client";
import type { HistoryEntry } from "../api/types";
import { useResourceStream } from "./useResourceStream";

export function useDetailResource<T>(
  key: string | undefined,
  fetchFn: (key: string, signal?: AbortSignal) => Promise<T>,
  ssePath: string,
) {
  const [data, setData] = useState<T | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [error, setError] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  const fetchData = useCallback(() => {
    if (!key) return;
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setError(false);
    fetchFn(key, controller.signal)
      .then((d) => {
        if (!controller.signal.aborted) setData(d);
      })
      .catch(() => {
        if (!controller.signal.aborted) setError(true);
      });
    api
      .history({ resourceId: key, limit: 10 }, controller.signal)
      .then((h) => {
        if (!controller.signal.aborted) setHistory(h);
      })
      .catch(() => {});
  }, [key, fetchFn]);

  useEffect(() => {
    fetchData();
    return () => {
      abortRef.current?.abort();
    };
  }, [fetchData]);

  useResourceStream(ssePath, fetchData);

  return { data, history, error };
}
