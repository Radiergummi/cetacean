import type { Service } from "../../api/types";
import { serviceUpdateStatus } from "../../lib/deriveServiceState";
import { formatRelativeDate } from "../../lib/format";
import InfoCard from "../InfoCard";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";

export function ServiceStatusCard({ service }: { service: Service }) {
  const { label, state } = serviceUpdateStatus(service);
  const ts = service.UpdateStatus?.CompletedAt || service.UpdateStatus?.StartedAt;
  const msg = service.UpdateStatus?.Message;

  return (
    <InfoCard
      label="Status"
      value={
        <div className="flex flex-col">
          <span
            data-state={state}
            className="text-base font-medium text-green-600 data-[state=paused]:text-amber-600 data-[state=rollback_completed]:text-amber-600 data-[state=rollback_paused]:text-amber-600 data-[state=rollback_started]:text-amber-600 data-[state=updating]:text-blue-600 dark:text-green-400 dark:data-[state=paused]:text-amber-400 dark:data-[state=rollback_completed]:text-amber-400 dark:data-[state=rollback_paused]:text-amber-400 dark:data-[state=rollback_started]:text-amber-400 dark:data-[state=updating]:text-blue-400"
          >
            {label}
          </span>
          {ts && <span className="text-xs text-muted-foreground">{formatRelativeDate(ts)}</span>}
          {msg && label !== "Stable" && (
            <Tooltip>
              <TooltipTrigger
                render={<span className="truncate text-xs text-muted-foreground">{msg}</span>}
              />
              <TooltipContent>{msg}</TooltipContent>
            </Tooltip>
          )}
        </div>
      }
    />
  );
}
