import { Spinner } from "@/components/Spinner";
import { Suspense, useEffect, useRef, useState, type ReactNode } from "react";

function Loading() {
  return (
    <div className="flex h-full items-center justify-center">
      <Spinner className="size-5 text-muted-foreground" />
    </div>
  );
}

/**
 * Holds a graph's box and mounts it only once it has been scrolled into view,
 * so React Flow and ELK are fetched when the graph is actually looked at
 * rather than alongside the page. Children stay unrendered until then, which
 * is what keeps a `lazy()` graph's chunk unrequested.
 */
export function GraphFrame({ children }: { children: ReactNode }) {
  const frame = useRef<HTMLDivElement>(null);
  const [seen, setSeen] = useState(false);

  useEffect(() => {
    const element = frame.current;

    if (!element || seen) {
      return;
    }

    const observer = new IntersectionObserver((entries) => {
      if (entries.some(({ isIntersecting }) => isIntersecting)) {
        setSeen(true);
      }
    });

    observer.observe(element);

    return () => observer.disconnect();
  }, [seen]);

  return (
    <div
      ref={frame}
      className="h-96 overflow-hidden rounded-lg border"
    >
      {seen ? <Suspense fallback={<Loading />}>{children}</Suspense> : <Loading />}
    </div>
  );
}
