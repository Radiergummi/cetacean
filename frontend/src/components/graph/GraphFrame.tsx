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
 * Mounts its children only once scrolled into view, which keeps a `lazy()`
 * graph's chunk — React Flow and ELK — unrequested until it is looked at. The
 * frame follows the window: fixed, a large stack fits at no legible zoom.
 */
export function GraphFrame({ children }: { children: ReactNode }) {
  const frame = useRef<HTMLDivElement>(null);
  const [seen, setSeen] = useState(false);

  useEffect(() => {
    const element = frame.current;

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
  }, []);

  return (
    <div
      ref={frame}
      data-testid="graph-frame"
      className="h-[clamp(20rem,60vh,44rem)] overflow-hidden rounded-lg border"
    >
      {seen ? <Suspense fallback={<Loading />}>{children}</Suspense> : <Loading />}
    </div>
  );
}
