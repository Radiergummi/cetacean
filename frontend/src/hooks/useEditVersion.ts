import { currentETag } from "@/api/client";
import { useMemo, useRef } from "react";

/**
 * Pins the version of the representation at `path` when an edit opens, so its
 * save carries `If-Match` and is refused if someone else changed it meanwhile.
 */
export function useEditVersion(path: string) {
  const version = useRef<Promise<string | undefined>>(Promise.resolve(undefined));

  return useMemo(
    () => ({
      capture() {
        version.current = currentETag(path);
      },
      ifMatch: () => version.current,
    }),
    [path],
  );
}
