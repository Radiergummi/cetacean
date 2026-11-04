import { useUrlParam } from "./useUrlParam";
import { useCallback, useEffect, useRef, useState } from "react";

const debounceMilliseconds = 300;

/**
 * URL parameter whose writes wait out a pause in typing. A change to the URL
 * from elsewhere, as by Back, drops a write still waiting.
 *
 * Returns [urlValue, setValue, external]: external says the URL holds a value
 * this hook did not write, so a field showing it should take it.
 */
export function useDebouncedUrlParam(key: string): [string, (value: string) => void, boolean] {
  const [urlValue, setUrlValue] = useUrlParam(key, "");
  const [written, setWritten] = useState(urlValue);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const external = urlValue !== written;

  useEffect(() => {
    if (urlValue !== written) {
      clearTimeout(timerRef.current);
    }
  }, [urlValue, written]);

  const setValue = useCallback(
    (value: string) => {
      clearTimeout(timerRef.current);

      timerRef.current = setTimeout(() => {
        setWritten(value);
        setUrlValue(value);
      }, debounceMilliseconds);
    },
    [setUrlValue],
  );

  // Cleanup timer on Unmount
  useEffect(() => () => clearTimeout(timerRef.current), []);

  return [urlValue, setValue, external];
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
  const [urlValue, setUrlValue, external] = useDebouncedUrlParam(key);
  const [inputValue, setInputValue] = useState(urlValue);
  const [lastUrlValue, setLastUrlValue] = useState(urlValue);

  // Sync the input when the URL changes externally (browser back/forward),
  // not when this hook's own write lands behind newer typing. Adjusting during
  // render: an effect committed a render showing the old text first.
  if (urlValue !== lastUrlValue) {
    setLastUrlValue(urlValue);

    if (external) {
      setInputValue(urlValue);
    }
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
