import { useState, useEffect, useCallback } from "react";
import { api } from "../api/client";
import type { HistoryEntry } from "../api/types";
import { useResourceStream } from "./useResourceStream";

export function useDetailResource<T>(
  key: string | undefined,
  fetchFn: (key: string) => Promise<T>,
  ssePath: string,
) {
  const [data, setData] = useState<T | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [error, setError] = useState(false);

  const fetchData = useCallback(() => {
    if (!key) return;
    fetchFn(key)
      .then(setData)
      .catch(() => setError(true));
    api
      .history({ resourceId: key, limit: 10 })
      .then(setHistory)
      .catch(() => {});
  }, [key, fetchFn]);

  useEffect(fetchData, [fetchData]);

  useResourceStream(ssePath, fetchData);

  return { data, history, error };
}
