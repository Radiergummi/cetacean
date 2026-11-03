import { useUrlParam } from "./useUrlParam";
import { useCallback, useEffect, useRef, useState } from "react";

const debounceMilliseconds = 300;

/**
 * URL parameter whose writes wait out a pause in typing. A change to the URL
 * from elsewhere, as by Back, drops a write still waiting.
 *
 * Returns [urlValue, setValue].
 */
export function useDebouncedUrlParam(key: string): [string, (value: string) => void] {
  const [urlValue, setUrlValue] = useUrlParam(key, "");
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const writtenRef = useRef(urlValue);

  useEffect(() => {
    if (urlValue !== writtenRef.current) {
      clearTimeout(timerRef.current);
      writtenRef.current = urlValue;
    }
  }, [urlValue]);

  const setValue = useCallback(
    (value: string) => {
      clearTimeout(timerRef.current);

      timerRef.current = setTimeout(() => {
        writtenRef.current = value;
        setUrlValue(value);
      }, debounceMilliseconds);
    },
    [setUrlValue],
  );

  // Cleanup timer on Unmount
  useEffect(() => () => clearTimeout(timerRef.current), []);

  return [urlValue, setValue];
}

/**
 * URL-backed search parameter with debounced URL updates.
 *
 * Returns [inputValue, debouncedValue, setInputValue].
 * - inputValue: updates immediately on every keystroke (for the input field)
 * - debouncedValue: updates the URL after debounceMilliseconds (for data fetching)
 * - setInputValue: setter for both (clear button, etc.)
 */
export function useSearchParam(key: string): [string, string, (value: string) => void] {
  const [urlValue, setUrlValue] = useDebouncedUrlParam(key);
  const [inputValue, setInputValue] = useState(urlValue);
  const [lastUrlValue, setLastUrlValue] = useState(urlValue);

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
      setUrlValue(value);
    },
    [setUrlValue],
  );

  return [inputValue, urlValue, setValue];
}
