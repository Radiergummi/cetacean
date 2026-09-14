import type { TraefikIntegration } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { lazy, Suspense, useEffect, useRef, useState } from "react";

const TraefikGraph = lazy(() => import("./TraefikGraph"));

function Loading() {
  return (
    <div className="flex h-full items-center justify-center">
      <Spinner className="size-5 text-muted-foreground" />
    </div>
  );
}

/**
 * Holds the graph's box and mounts it only once it has been scrolled into
 * view, so React Flow and ELK are fetched when the graph is actually looked
 * at rather than alongside the page.
 */
export function TraefikGraphFrame({ integration }: { integration: TraefikIntegration }) {
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
      className="h-96 rounded-lg border"
    >
      {seen ? (
        <Suspense fallback={<Loading />}>
          <TraefikGraph integration={integration} />
        </Suspense>
      ) : (
        <Loading />
      )}
    </div>
  );
}
