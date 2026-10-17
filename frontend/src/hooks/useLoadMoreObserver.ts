import { useLatestRef } from "./useLatestRef";
import { type RefObject, useEffect } from "react";

interface Options {
  /** The scroll container to observe against; the viewport when absent. */
  rootRef?: RefObject<Element | null> | undefined;
  enabled?: boolean | undefined;
}

/**
 * Calls `onLoadMore` whenever the target comes within 200px of view.
 */
export function useLoadMoreObserver(
  targetRef: RefObject<Element | null>,
  onLoadMore: (() => void) | undefined,
  { rootRef, enabled = true }: Options = {},
) {
  const onLoadMoreRef = useLatestRef(onLoadMore);

  useEffect(() => {
    if (!enabled || !onLoadMoreRef.current || !targetRef.current) {
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some(({ isIntersecting }) => isIntersecting)) {
          onLoadMoreRef.current?.();
        }
      },
      { root: rootRef?.current ?? null, rootMargin: "200px" },
    );

    observer.observe(targetRef.current);

    return () => observer.disconnect();
  }, [enabled, onLoadMoreRef, targetRef, rootRef]);
}
