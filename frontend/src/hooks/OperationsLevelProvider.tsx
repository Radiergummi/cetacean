import { OperationsLevelContext, type OperationsLevelState } from "./useOperationsLevel";
import { apiPath } from "@/lib/basePath";
import type React from "react";
import { useEffect, useState } from "react";

export function OperationsLevelProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<OperationsLevelState>({
    level: 0,
    loading: true,
    error: false,
  });

  useEffect(() => {
    let cancelled = false;
    let attempt = 0;

    function tryFetch() {
      fetch(apiPath("/-/health"))
        .then((response) => response.json())
        .then((data) => {
          if (!cancelled) {
            setState({
              level: data.operationsLevel ?? 0,
              loading: false,
              error: false,
            });
          }
        })
        .catch(() => {
          if (cancelled) {
            return;
          }

          attempt++;

          if (attempt < 4) {
            const delay = 1000 * 2 ** (attempt - 1);
            setTimeout(tryFetch, delay);
          } else {
            setState({
              level: 0,
              loading: false,
              error: true,
            });
          }
        });
    }

    tryFetch();

    return () => {
      cancelled = true;
    };
  }, []);

  return <OperationsLevelContext value={state}>{children}</OperationsLevelContext>;
}
