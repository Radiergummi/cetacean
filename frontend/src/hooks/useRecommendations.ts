import { api } from "@/api/client";
import type { Recommendation, RecommendationSummary } from "@/api/types";
import { queryClient } from "@/lib/queryClient";
import { useQuery } from "@tanstack/react-query";

interface RecommendationsState {
  items: Recommendation[];
  summary: RecommendationSummary;
  total: number;
  hasData: boolean;
}

const emptyState: RecommendationsState = {
  items: [],
  summary: { critical: 0, warning: 0, info: 0 },
  total: 0,
  hasData: false,
};

export const recommendationsQueryKey = ["recommendations"] as const;

export function invalidateRecommendations() {
  return queryClient.invalidateQueries({ queryKey: recommendationsQueryKey });
}

export function useRecommendations(): RecommendationsState {
  const { data } = useQuery({
    queryKey: recommendationsQueryKey,
    queryFn: async () => {
      const response = await api.recommendations();
      return {
        items: response.items ?? [],
        summary: response.summary,
        total: response.total,
        hasData: true,
      } satisfies RecommendationsState;
    },
    staleTime: 60_000,
  });

  return data ?? emptyState;
}
