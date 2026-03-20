import { api } from "@/api/client";
import { useEffect, useState } from "react";

export function useGaugeValue(query: string, enabled: boolean) {
  const [value, setValue] = useState<number | null>(null);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    function poll() {
      api
        .metricsQuery(query)
        .then((response) => {
          const raw = response.data?.result?.[0]?.value?.[1];
          setValue(raw != null ? Number(raw) : null);
        })
        .catch(() => setValue(null));
    }

    poll();
    const interval = setInterval(poll, 30_000);

    return () => clearInterval(interval);
  }, [query, enabled]);

  return value;
}
