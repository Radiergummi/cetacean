import { getErrorMessage } from "@/lib/utils";
import { useEffect, useRef, useState } from "react";

export function useAsyncAction() {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  async function execute(action: () => Promise<unknown>, errorMessage: string) {
    setLoading(true);
    setError(null);

    try {
      await action();
    } catch (thrown) {
      if (mountedRef.current) {
        setError(getErrorMessage(thrown, errorMessage));
      }
    } finally {
      if (mountedRef.current) {
        setLoading(false);
      }
    }
  }

  return { loading, error, execute };
}
