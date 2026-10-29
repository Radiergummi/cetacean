import { type AttestedKind, api } from "@/api/client";
import { useQuery } from "@tanstack/react-query";

/**
 * What the image's registry holds about how a service's or task's image was
 * built. The image badge and the Manifest section share the one request.
 */
export function useProvenance(kind: AttestedKind, id: string) {
  return useQuery({
    queryKey: ["provenance", kind, id],
    queryFn: ({ signal }) => api.provenance(kind, id, signal),
  });
}
