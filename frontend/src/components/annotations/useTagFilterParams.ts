import { setTagParams } from "@/api/client";
import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";

/**
 * A list's tag filter in its URL: the `tag`s, whether `matchAny` widens them,
 * and a setter writing both in place.
 */
export function useTagFilterParams() {
  const [params, setParams] = useSearchParams();

  const setTagFilter = useCallback(
    (tags: string[], matchAny: boolean) =>
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);

          setTagParams(next, tags, matchAny);

          return next;
        },
        { replace: true },
      ),
    [setParams],
  );

  return {
    tags: [...new Set(params.getAll("tag"))],
    matchAny: params.get("matchAny") === "true",
    setTagFilter,
  };
}
