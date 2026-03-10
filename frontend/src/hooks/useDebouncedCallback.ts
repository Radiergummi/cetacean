import { useCallback, useEffect, useRef } from "react";

/** Returns a debounced version of `fn` that coalesces calls within `ms`. */
export default function useDebouncedCallback(fn: () => void, ms: number): () => void {
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const fnRef = useRef(fn);
  fnRef.current = fn;

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  return useCallback(() => {
    if (timerRef.current) return;
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      fnRef.current();
    }, ms);
  }, [ms]);
}
