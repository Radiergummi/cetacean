import type { Service } from "../../api/types";
import { serviceUpdateStatus } from "../../lib/deriveServiceState";
import { formatRelativeDate } from "../../lib/format";
import InfoCard from "../InfoCard";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";

export function ServiceStatusCard({ service }: { service: Service }) {
  const { label, state } = serviceUpdateStatus(service);
  const ts = service.UpdateStatus?.CompletedAt || service.UpdateStatus?.StartedAt;
  const message = service.UpdateStatus?.Message;

  return (
    <InfoCard
      label="Status"
      value={
        <div className="flex flex-col">
          <span
            data-state={state}
            className="text-base font-medium text-status-ok data-[state=paused]:text-status-warning data-[state=rollback_completed]:text-status-warning data-[state=rollback_paused]:text-status-warning data-[state=rollback_started]:text-status-warning data-[state=updating]:text-status-info"
          >
            {label}
          </span>
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
