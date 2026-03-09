import { useSearchParams } from "react-router-dom";
import { useCallback, useEffect, useRef, useState } from "react";

const DEBOUNCE_MS = 300;

/**
 * URL-backed search parameter with debounced URL updates.
 *
 * Returns [inputValue, debouncedValue, setInputValue].
 * - inputValue: updates immediately on every keystroke (for the input field)
 * - debouncedValue: updates the URL after DEBOUNCE_MS (for data fetching)
 * - setInputValue: setter for both (clear button, etc.)
 */
export function useSearchParam(key: string): [string, string, (value: string) => void] {
  const [params, setParams] = useSearchParams();
  const urlValue = params.get(key) ?? "";
  const [inputValue, setInputValue] = useState(urlValue);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  // Sync input when URL changes externally (e.g. browser back/forward)
  useEffect(() => {
    setInputValue(urlValue);
  }, [urlValue]);

  const setValue = useCallback(
    (v: string) => {
      setInputValue(v);
      clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => {
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
      }, DEBOUNCE_MS);
    },
    [key, setParams],
  );

  // Cleanup timer on unmount
  useEffect(() => () => clearTimeout(timerRef.current), []);

  return [inputValue, urlValue, setValue];
}
