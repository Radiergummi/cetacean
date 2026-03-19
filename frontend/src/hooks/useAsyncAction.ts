import { getErrorMessage } from "@/lib/utils";
import { useState } from "react";

export function useAsyncAction() {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function execute(action: () => Promise<unknown>, errorMessage: string) {
    setLoading(true);
    setError(null);

    try {
      await action();
    } catch (thrown) {
      setError(getErrorMessage(thrown, errorMessage));
    } finally {
      setLoading(false);
    }
  }

  return { loading, error, execute };
}
