import { useRecommendations } from "@/hooks/useRecommendations";
import { ArrowRight } from "lucide-react";
import { Link } from "react-router-dom";

export default function RecommendationSummary() {
  const { summary, total, hasData } = useRecommendations();

  if (total === 0 || !hasData) {
    return null;
  }

  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">Recommendations</h3>
        <Link
          to="/recommendations"
          className="text-xs text-muted-foreground hover:text-foreground"
        >
          View all <ArrowRight className="ml-0.5 inline size-3" />
        </Link>
      </div>
      <div className="mt-1.5 flex gap-3 text-xs text-muted-foreground">
        {summary.critical > 0 && (
          <span>
            <span className="mr-1 inline-flex min-w-5 items-center justify-center rounded-full bg-red-600 px-1.5 py-0.5 text-[10px] leading-none font-medium text-white dark:bg-red-500">
              {summary.critical}
            </span>
            critical
          </span>
        )}
        {summary.warning > 0 && (
          <span>
            <span className="mr-1 inline-flex min-w-5 items-center justify-center rounded-full bg-amber-500 px-1.5 py-0.5 text-[10px] leading-none font-medium text-white dark:bg-amber-500">
              {summary.warning}
            </span>
            warnings
          </span>
        )}
        {summary.info > 0 && (
          <span>
            <span className="mr-1 inline-flex min-w-5 items-center justify-center rounded-full bg-blue-600 px-1.5 py-0.5 text-[10px] leading-none font-medium text-white dark:bg-blue-500">
              {summary.info}
            </span>
            info
          </span>
        )}
      </div>
    </div>
  );
}
