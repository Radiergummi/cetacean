import { showErrorToast } from "@/lib/showErrorToast";
import { getErrorMessage } from "@/lib/utils";
import { useEffect, useRef, useState } from "react";

interface AsyncActionOptions {
  toast?: boolean;
}

export function useAsyncAction(options?: AsyncActionOptions) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [cause, setCause] = useState<unknown>(null);
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
    setCause(null);

    try {
      await action();
    } catch (caught) {
      if (mountedRef.current) {
        setError(getErrorMessage(caught, errorMessage));
        setCause(caught);

        if (options?.toast) {
          showErrorToast(caught, errorMessage);
        }
      }
    } finally {
      if (mountedRef.current) {
        setLoading(false);
      }
    }
  }

  return { loading, error, cause, execute };
}
