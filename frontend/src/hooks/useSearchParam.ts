import { useUrlParam } from "./useUrlParam";
import { useCallback, useEffect, useRef, useState } from "react";

const debounceMilliseconds = 300;

/**
 * URL-backed search parameter with debounced URL updates.
 *
 * Returns [inputValue, debouncedValue, setInputValue].
 * - inputValue: updates immediately on every keystroke (for the input field)
 * - debouncedValue: updates the URL after debounceMilliseconds (for data fetching)
 * - setInputValue: setter for both (clear button, etc.)
 */
export function useSearchParam(key: string): [string, string, (value: string) => void] {
  const [urlValue, setUrlValue] = useUrlParam(key, "");
  const [inputValue, setInputValue] = useState(urlValue);
  const [lastUrlValue, setLastUrlValue] = useState(urlValue);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  // Sync the input when the URL changes externally (browser back/forward).
  // Adjusting during render rather than in an effect: the effect committed a
  // render showing the old text before correcting it.
  if (urlValue !== lastUrlValue) {
    setLastUrlValue(urlValue);
    setInputValue(urlValue);
  }

  const setValue = useCallback(
    (value: string) => {
      setInputValue(value);
      clearTimeout(timerRef.current);

      timerRef.current = setTimeout(() => setUrlValue(value), debounceMilliseconds);
    },
    [setUrlValue],
  );

  // Cleanup timer on Unmount
  useEffect(() => () => clearTimeout(timerRef.current), []);

  return [inputValue, urlValue, setValue];
}
