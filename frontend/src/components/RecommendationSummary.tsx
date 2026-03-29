import { useRecommendations } from "@/hooks/useRecommendations";
import { Link } from "react-router-dom";

export default function RecommendationSummary() {
  const { summary, total, hasData } = useRecommendations();

  if (total === 0 || !hasData) {
    return null;
  }

  const borderColor =
    summary.critical > 0
      ? "border-red-500/50"
      : summary.warning > 0
        ? "border-amber-500/50"
        : "border-blue-500/50";

  return (
    <div className={`rounded-lg border ${borderColor} bg-card p-4`}>
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">Recommendations</h3>
        <Link
          to="/recommendations"
          className="text-xs text-muted-foreground hover:text-foreground"
        >
          View all →
        </Link>
      </div>
      <div className="mt-1 flex gap-3 text-xs">
        {summary.critical > 0 && <span className="text-red-600">{summary.critical} critical</span>}
        {summary.warning > 0 && <span className="text-amber-600">{summary.warning} warnings</span>}
        {summary.info > 0 && <span className="text-blue-600">{summary.info} info</span>}
      </div>
    </div>
  );
}
