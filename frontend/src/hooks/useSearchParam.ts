import { useSearchParams } from "react-router-dom";
import { useCallback } from "react";

export function useSearchParam(key: string): [string, (value: string) => void] {
  const [params, setParams] = useSearchParams();
  const value = params.get(key) ?? "";
  const setValue = useCallback(
    (v: string) => {
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (v) {
            next.set(key, v);
          } else {
            next.delete(key);
          }
          return next;
        },
        { replace: true },
      );
    },
    [key, setParams],
  );
  return [value, setValue];
}
