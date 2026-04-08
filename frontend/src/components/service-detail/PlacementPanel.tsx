import type { Placement } from "../../api/types";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { humanizeConstraint } from "@/lib/placementConstraints";

export function PlacementPanel({ placement }: { placement: Placement }) {
  const constraints = placement.Constraints ?? [];
  const preferences = placement.Preferences ?? [];

  return (
    <div className="flex flex-col gap-3">
      {constraints.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {constraints.map((constraint) => {
            const humanized = humanizeConstraint(constraint);
            const pillClassName =
              "inline-flex items-center rounded-lg border px-3 py-2 text-sm data-exclude:border-red-200 data-exclude:bg-red-50 data-exclude:text-red-800 dark:data-exclude:border-red-800 dark:data-exclude:bg-red-950/30 dark:data-exclude:text-red-300";

            if (!humanized) {
              return (
                <span
                  key={constraint}
                  className={pillClassName}
                >
                  {constraint}
                </span>
              );
            }

            return (
              <Tooltip key={constraint}>
                <TooltipTrigger
                  data-exclude={humanized.exclude || undefined}
                  className={pillClassName}
                >
                  {humanized.label}
                </TooltipTrigger>
                <TooltipContent className="font-mono">{constraint}</TooltipContent>
              </Tooltip>
            );
          })}
        </div>
      )}

      {placement.MaxReplicas != null && placement.MaxReplicas > 0 && (
        <div className="inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-sm">
          <span className="text-muted-foreground">Max replicas per node:</span>
          <span className="font-semibold tabular-nums">{placement.MaxReplicas}</span>
        </div>
      )}

      {preferences.length > 0 && (
        <div className="space-y-1">
          <div className="text-xs font-medium text-muted-foreground">Spread preferences</div>
          <div className="flex flex-wrap gap-2">
            {preferences.map(({ Spread }, index) => (
              <span
                key={index}
                className="inline-flex items-center rounded-md border px-2.5 py-1 font-mono text-xs"
              >
                {Spread?.SpreadDescriptor}
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
