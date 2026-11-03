import { api } from "@/api/client";
import { useQuery } from "@tanstack/react-query";

/**
 * Whether the caller may create a cluster-wide annotation, read from the link
 * `/cluster` sends; false until it has answered, so nothing is offered on a
 * guess. Whether they may create any is the link on the list a page reads.
 */
export function useClusterAnnotatable(): boolean {
  const { data = false } = useQuery({
    queryKey: ["cluster"],
    queryFn: () => api.cluster(),
    select: ({ annotatable }) => annotatable ?? false,
    staleTime: 60_000,
  });

  return data;
}
