import {ArrowRight} from "lucide-react";
import type {Service, SpecChange} from "../../api/types";
import {timeAgo} from "../TimeAgo";

export function DeploymentChanges({
  changes,
  updateStatus,
}: {
  changes: SpecChange[];
  updateStatus?: Service["UpdateStatus"];
}) {
  const ts = updateStatus?.CompletedAt || updateStatus?.StartedAt;
  const deploymentLabels: Record<string, string> = {
    updating: "In progress",
    rollback_started: "Rolling back",
    rollback_paused: "Rollback paused",
    rollback_completed: "Rolled back",
  };
  const stateLabel = deploymentLabels[updateStatus?.State ?? ""] ?? "Completed";

  return (
    <div className="space-y-3">
      {ts && (
        <p className="text-sm text-muted-foreground">
          {stateLabel} {timeAgo(ts)}
        </p>
      )}
      <div className="divide-y rounded-lg border">
        {changes.map(({field, new: change, old}, index) => (
          <div
            key={index}
            className="flex items-center gap-2 px-3 py-2 text-sm"
          >
            <span className="min-w-40 shrink-0 font-medium">{field}</span>
            {old && change ? (
              <>
                <span className="font-mono text-xs text-red-600 line-through dark:text-red-400">
                  {old}
                </span>
                <ArrowRight className="size-3 shrink-0 text-muted-foreground"/>
                <span className="font-mono text-xs text-green-600 dark:text-green-400">
                  {change}
                </span>
              </>
            ) : old ? (
              <span className="font-mono text-xs text-red-600 dark:text-red-400">{old}</span>
            ) : (
              <span className="font-mono text-xs text-green-600 dark:text-green-400">{change}</span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
