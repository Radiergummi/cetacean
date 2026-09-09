import type { Service, SpecChange } from "@/api/types.ts";
import { formatRelativeDate } from "@/lib/format.ts";
import { handleCopyWithTemplates, renderSwarmTemplate } from "@/lib/swarmTemplates";
import { ArrowRight } from "lucide-react";

export function DeploymentChanges({
  changes,
  updateStatus,
}: {
  changes: SpecChange[];
  updateStatus?: Service["UpdateStatus"] | undefined;
}) {
  const timestamp = updateStatus?.CompletedAt || updateStatus?.StartedAt;
  const deploymentLabels: Record<string, string> = {
    updating: "In progress",
    rollback_started: "Rolling back",
    rollback_paused: "Rollback paused",
    rollback_completed: "Rolled back",
  };
  const stateLabel = deploymentLabels[updateStatus?.State ?? ""] ?? "Completed";

  return (
    <div className="space-y-3">
      {timestamp && (
        <p className="text-sm text-muted-foreground">
          {stateLabel} {formatRelativeDate(timestamp)}
        </p>
      )}

      <div
        className="divide-y rounded-lg border"
        onCopy={handleCopyWithTemplates}
      >
        {changes.map(({ field, new: change, old }, index) => (
          <div
            key={index}
            className="flex items-center gap-2 overflow-hidden px-3 py-2 text-sm"
          >
            <span className="min-w-40 shrink-0 font-medium">{field}</span>
            {old && change ? (
              <>
                <span
                  className="truncate font-mono text-xs text-status-danger line-through"
                  title={old}
                >
                  {renderSwarmTemplate(old)}
                </span>
                <ArrowRight className="size-3 shrink-0 text-muted-foreground" />
                <span
                  className="truncate font-mono text-xs text-status-ok"
                  title={change}
                >
                  {renderSwarmTemplate(change)}
                </span>
              </>
            ) : old ? (
              <span
                className="truncate font-mono text-xs text-status-danger"
                title={old}
              >
                {renderSwarmTemplate(old)}
              </span>
            ) : (
              <span
                className="truncate font-mono text-xs text-status-ok"
                title={change ?? ""}
              >
                {renderSwarmTemplate(change ?? "")}
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
