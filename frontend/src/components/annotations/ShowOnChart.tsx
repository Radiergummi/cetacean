import { chartPath } from "./annotationFields";
import type { HistoryEntry } from "@/api/types";
import { isPrometheusReady, useMonitoringStatus } from "@/hooks/useMonitoringStatus";
import { ChartLine } from "lucide-react";
import { Link } from "react-router-dom";

/**
 * Opens the first resource a note names that has a metrics panel, its window
 * set around the note; nothing while metrics are off or no resource has one.
 */
export default function ShowOnChart({
  note,
  className,
}: {
  note: Pick<HistoryEntry, "resources" | "timestamp" | "until">;
  className?: string | undefined;
}) {
  const metrics = isPrometheusReady(useMonitoringStatus());
  const path = chartPath(note);

  if (!metrics || !path) {
    return null;
  }

  return (
    <Link
      to={path}
      className={className}
    >
      <ChartLine className="size-3.5" />
      Show on chart
    </Link>
  );
}
