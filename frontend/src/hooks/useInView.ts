import { type RefObject, useEffect, useState } from "react";

/**
 * Whether the target has scrolled into view yet. It latches: once seen, it
 * stays true, so whatever it gates is loaded once and kept.
 */
export function useInView(target: RefObject<Element | null>) {
  const [seen, setSeen] = useState(false);

  useEffect(() => {
    const element = target.current;

    if (!element) {
      return;
    }

    const observer = new IntersectionObserver((entries) => {
      if (entries.some(({ isIntersecting }) => isIntersecting)) {
        setSeen(true);
        observer.disconnect();
      }
    });

    observer.observe(element);

    return () => observer.disconnect();
  }, [target]);

  return seen;
}
