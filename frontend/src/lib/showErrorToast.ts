import { ApiError } from "@/api/client";
import { getErrorInfo } from "@/lib/errors";
import { toast } from "sonner";

/**
 * Show a toast notification for an API error.
 * Uses the error dictionary for known error codes,
 * falls back to the error message for unknown errors.
 */
export function showErrorToast(error: unknown, fallback: string): void {
  const code = error instanceof ApiError ? error.code : null;
  const info = getErrorInfo(code);

  if (info) {
    toast.error(info.title, { description: info.suggestion });
  } else {
    const message = error instanceof Error ? error.message : fallback;
    toast.error(message);
  }
}
