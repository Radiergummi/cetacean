import { emptyMethods } from "../api/client";
import { useMemo } from "react";

/**
 * Keeps a Set of methods or sub-resource names by reference while its contents
 * are unchanged. Every response builds a new Set, and handing that to consumers
 * would re-render them on each SSE refetch.
 */
export function useStableMethods(methods: ReadonlySet<string> | undefined) {
  const methodsKey = [...(methods ?? emptyMethods)].sort().join(",");

  return useMemo(() => new Set(methodsKey === "" ? [] : methodsKey.split(",")), [methodsKey]);
}
