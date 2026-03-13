import { useEffect, useRef } from "react";

type HotkeyMap = Record<string, () => void>;

function isEditing(event: KeyboardEvent): boolean {
  const target = event.target as HTMLElement;
  return (
    target.tagName === "INPUT" ||
    target.tagName === "TEXTAREA" ||
    target.tagName === "SELECT" ||
    target.isContentEditable
  );
}

/**
 * Register global keyboard shortcuts. Ignores events when focus is in an
 * input/textarea. Supports single keys ("?", "/") and two-key chords ("g n").
 * Chord timeout is 1 second.
 */
export function useHotkeys(hotkeys: HotkeyMap) {
  const hotkeysRef = useRef(hotkeys);
  hotkeysRef.current = hotkeys;

  const pendingRef = useRef<string | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (isEditing(event) || event.metaKey || event.ctrlKey || event.altKey) {
        return;
      }

      const map = hotkeysRef.current;
      const key = event.key;

      // Check for second key of a chord
      if (pendingRef.current) {
        const chord = `${pendingRef.current} ${key}`;
        pendingRef.current = null;
        if (timerRef.current) {
          clearTimeout(timerRef.current);
          timerRef.current = null;
        }
        if (map[chord]) {
          event.preventDefault();
          map[chord]();
          return;
        }
      }

      // Check for chord starters (keys that begin a multi-key sequence)
      const isChordStarter = Object.keys(map).some((k) => k.startsWith(key + " "));
      if (isChordStarter) {
        event.preventDefault();
        pendingRef.current = key;
        timerRef.current = setTimeout(() => {
          pendingRef.current = null;
        }, 1000);
        return;
      }

      // Single key match
      if (map[key]) {
        event.preventDefault();
        map[key]();
      }
    }

    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);
}
