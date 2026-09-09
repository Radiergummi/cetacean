import type { Service } from "../../api/types";
import { rolloutToneClass, serviceUpdateStatus } from "../../lib/deriveServiceState";
import { formatRelativeDate } from "../../lib/format";
import { cn } from "../../lib/utils";
import InfoCard from "../InfoCard";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";

export function ServiceStatusCard({ service }: { service: Service }) {
  const { label, state } = serviceUpdateStatus(service);
  const ts = service.UpdateStatus?.CompletedAt || service.UpdateStatus?.StartedAt;
  const message = service.UpdateStatus?.Message;

  return (
    <InfoCard
      label="Rollout"
      value={
        <div className="flex flex-col">
          <span className={cn("text-base font-medium", rolloutToneClass(state))}>{label}</span>
          {ts && <span className="text-xs text-muted-foreground">{formatRelativeDate(ts)}</span>}
          {message && label !== "Stable" && (
            <Tooltip>
              <TooltipTrigger
                render={<span className="truncate text-xs text-muted-foreground">{message}</span>}
              />
              <TooltipContent>{message}</TooltipContent>
            </Tooltip>
          )}
        </div>
      }
    />
  );
}
