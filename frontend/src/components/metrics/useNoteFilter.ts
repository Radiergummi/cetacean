import type { TimelineItem } from "@/lib/timeline";
import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";

/**
 * The timeline a panel's charts draw: without notes when `shown` is false,
 * and with only the notes carrying one of `tags` when any are given.
 */
export function filterNotes(items: TimelineItem[], shown: boolean, tags: string[]) {
  return items.filter(
    ({ kind, entry }) =>
      kind !== "annotation" ||
      (shown && (tags.length === 0 || tags.some((tag) => entry.tags?.includes(tag)))),
  );
}

/**
 * Whether the page's charts show notes and which tags narrow them, in the URL
 * as `notes=off` and `noteTag`: view state that belongs in a link.
 */
export function useNoteFilter() {
  const [params, setParams] = useSearchParams();

  const update = useCallback(
    (change: (next: URLSearchParams) => void) =>
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);

          change(next);

          return next;
        },
        { replace: true },
      ),
    [setParams],
  );

  const setShown = useCallback(
    (shown: boolean) => update((next) => (shown ? next.delete("notes") : next.set("notes", "off"))),
    [update],
  );

  const toggleTag = useCallback(
    (tag: string) =>
      update((next) => {
        const tags = next.getAll("noteTag");

        next.delete("noteTag");

        for (const kept of tags.includes(tag)
          ? tags.filter((other) => other !== tag)
          : [...tags, tag]) {
          next.append("noteTag", kept);
        }
      }),
    [update],
  );

  const tags = useMemo(() => params.getAll("noteTag"), [params]);

  return {
    shown: params.get("notes") !== "off",
    tags,
    setShown,
    toggleTag,
  };
}
