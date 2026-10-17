import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";

type Setter = (value: string | null) => void;

/**
 * One search parameter, read and written in place. Setting it empty or to the
 * default drops it from the URL; writes replace rather than push history.
 */
export function useUrlParam(key: string): [string | null, Setter];
export function useUrlParam(key: string, defaultValue: string): [string, Setter];
export function useUrlParam(key: string, defaultValue?: string): [string | null, Setter] {
  const [params, setParams] = useSearchParams();

  const setValue = useCallback(
    (value: string | null) => {
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);

          if (value && value !== defaultValue) {
            next.set(key, value);
          } else {
            next.delete(key);
          }

          return next;
        },
        { replace: true },
      );
    },
    [key, defaultValue, setParams],
  );

  return [params.get(key) ?? defaultValue ?? null, setValue];
}
