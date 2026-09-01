/**
 * The `get_recommendations` structured output, mirroring
 * internal/mcp.recommendationsResult and internal/recommendations.
 */
export interface RecommendationsResult {
  items: Recommendation[];
  total: number;
  summary: Summary;
}

export interface Recommendation {
  category: string;
  severity: Severity;
  scope: string;
  targetId: string;
  targetName: string;
  resource?: string;
  message: string;
  current?: number;
  configured?: number;
  suggested?: number;
  fixAction?: string;
}

export interface Summary {
  critical: number;
  warning: number;
  info: number;
}

export type Severity = "critical" | "warning" | "info";

/** Most serious first: the order a reader wants, not the order they arrive. */
export const severityOrder: Severity[] = ["critical", "warning", "info"];
