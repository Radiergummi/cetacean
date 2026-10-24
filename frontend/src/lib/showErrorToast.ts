import { ApiError } from "@/api/client";
import { getErrorInfo, withSuggestion } from "@/lib/errors";
import { toast } from "sonner";

/**
 * Show a toast notification for an API error.
 * Uses the error dictionary for known error codes, keeping the server's detail
 * ahead of the suggestion, and falls back to the error message otherwise.
 */
export function showErrorToast(error: unknown, fallback: string): void {
  const code = error instanceof ApiError ? error.code : null;
  const info = getErrorInfo(code);

  if (info) {
    const detail = error instanceof ApiError ? error.detail : "";
    toast.error(info.title, { description: withSuggestion(detail, info.suggestion) });
  } else {
    const message = error instanceof Error ? error.message : fallback;
    toast.error(message);
  }
}
