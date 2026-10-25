import type { ImageUpdate } from "@/api/types";
import { CloudDownload } from "lucide-react";

export const newerImageMessage = "A newer image exists for this tag";

/**
 * Marks a service whose image tag names a newer image than the one it runs.
 */
export function NewerImageBadge({ update }: { update: ImageUpdate }) {
  const shortDigest = update.latestDigest.replace(/^sha256:/, "").slice(0, 12);

  return (
    <span
      className="inline-flex shrink-0 items-center gap-1 rounded-md bg-status-info/10 px-1.5 py-0.5 text-xs font-medium text-status-info"
      title={`${newerImageMessage}: ${update.image} is now ${shortDigest}, checked ${update.checkedAt}`}
    >
      <CloudDownload
        className="size-3"
        aria-hidden="true"
      />
      Newer image
      <span className="sr-only">: {newerImageMessage}</span>
    </span>
  );
}
