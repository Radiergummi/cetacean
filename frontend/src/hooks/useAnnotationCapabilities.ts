import { api } from "@/api/client";
import { useQuery } from "@tanstack/react-query";

const nothing = { create: false, clusterWide: false };

/**
 * What annotations the caller may create, as the server says; nothing until
 * it has answered, so no control is offered on a guess.
 */
export function useAnnotationCapabilities(): { create: boolean; clusterWide: boolean } {
  const { data } = useQuery({
    queryKey: ["annotation-capabilities"],
    queryFn: () => api.annotationCapabilities(),
    staleTime: 60_000,
  });

  return data ?? nothing;
}
