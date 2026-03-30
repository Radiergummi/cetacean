import { api } from "@/api/client";
import type { Recommendation, RecommendationSummary, RecommendationsResponse } from "@/api/types";
import { useCallback, useEffect, useState } from "react";
import { useLocation } from "react-router-dom";

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

let cached: RecommendationsState = emptyState;
let cacheTime = 0;
let inflight: Promise<RecommendationsResponse> | null = null;
const cacheTTL = 60_000;

/** Subscribers notified when the cache is invalidated. */
const subscribers = new Set<() => void>();

async function fetchCached(): Promise<RecommendationsState> {
  const now = Date.now();

  if (cached.hasData && now - cacheTime < cacheTTL) {
    return cached;
  }

  if (!inflight) {
    inflight = api.recommendations().finally(() => {
      inflight = null;
    });
  }

  const response = await inflight;

  cached = {
    items: response.items ?? [],
    summary: response.summary,
    total: response.total,
    hasData: true,
  };
  cacheTime = Date.now();

  return cached;
}

/**
 * Invalidates the module-level cache and re-fetches for all consumers.
 * Call this after applying a recommendation fix.
 */
export function invalidateRecommendations() {
  cacheTime = 0;

  fetchCached()
    .then(() => {
      for (const notify of subscribers) {
        notify();
      }
    })
    .catch(console.warn);
}

export function useRecommendations(): RecommendationsState {
  const { pathname } = useLocation();
  const [state, setState] = useState<RecommendationsState>(cached.hasData ? cached : emptyState);

  const refresh = useCallback(() => {
    fetchCached()
      .then((result) => setState(result))
      .catch(console.warn);
  }, []);

  useEffect(() => {
    subscribers.add(refresh);

    return () => {
      subscribers.delete(refresh);
    };
  }, [refresh]);

  useEffect(() => {
    let cancelled = false;

    fetchCached()
      .then((result) => {
        if (!cancelled) {
          setState(result);
        }
      })
      .catch(console.warn);

    return () => {
      cancelled = true;
    };
  }, [pathname]);

  return state;
}
