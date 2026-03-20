import type { Service } from "../../api/types";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

export type PlacementShape = NonNullable<Service["Spec"]["TaskTemplate"]["Placement"]>;

function humanizeConstraint(raw: string): { label: string; exclude: boolean } | null {
  const match = raw.match(/^(.+?)\s*(==|!=)\s*(.+)$/);

  if (!match) {
    return null;
  }

  const [, field, op, value] = match;
  const exclude = op === "!=";

  if (field === "node.role") {
    if (value === "manager" && !exclude) {
      return {
        label: "Manager nodes only",
        exclude,
      };
    }

    if (value === "worker" && !exclude) {
      return {
        label: "Worker nodes only",
        exclude,
      };
    }

    if (value === "manager" && exclude) {
      return {
        label: "Exclude manager nodes",
        exclude,
      };
    }

    if (value === "worker" && exclude) {
      return {
        label: "Exclude worker nodes",
        exclude,
      };
    }
  }

  if (field === "node.hostname") {
    return {
      label: exclude ? `Exclude node ${value}` : `Node: ${value}`,
      exclude,
    };
  }

  if (field === "node.id") {
    return {
      label: exclude ? `Exclude node ID ${value}` : `Node ID: ${value}`,
      exclude,
    };
  }

  if (field === "node.platform.os") {
    return {
      label: exclude ? `Exclude OS ${value}` : `OS: ${value}`,
      exclude,
    };
  }

  if (field === "node.platform.arch") {
    return {
      label: exclude ? `Exclude arch ${value}` : `Arch: ${value}`,
      exclude,
    };
  }

  if (field.startsWith("node.labels.")) {
    const key = field.slice("node.labels.".length);

    return {
      label: exclude ? `${key} \u2260 ${value}` : `${key} = ${value}`,
      exclude,
    };
  }

  if (field.startsWith("engine.labels.")) {
    const key = field.slice("engine.labels.".length);

    return {
      label: exclude ? `engine ${key} \u2260 ${value}` : `engine ${key} = ${value}`,
      exclude,
    };
  }

  return null;
}

export function PlacementPanel({ placement }: { placement: PlacementShape }) {
  const constraints = placement.Constraints ?? [];
  const preferences = placement.Preferences ?? [];
  const hasContent = constraints.length > 0 || placement.MaxReplicas || preferences.length > 0;

  if (!hasContent) {
    return (
      <div className="flex items-center justify-center rounded-md bg-muted/75 p-3 text-muted-foreground">
        <p className="text-sm text-muted-foreground">No placement constraints.</p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {constraints.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {constraints.map((constraint) => {
            const humanized = humanizeConstraint(constraint);
            const pill = (
              <span
                key={constraint}
                data-exclude={humanized?.exclude || undefined}
                className="inline-flex items-center rounded-lg border px-3 py-2 text-sm data-exclude:border-red-200 data-exclude:bg-red-50 data-exclude:text-red-800 dark:data-exclude:border-red-800 dark:data-exclude:bg-red-950/30 dark:data-exclude:text-red-300"
              >
                {humanized?.label ?? constraint}
              </span>
            );

            if (!humanized) {
              return pill;
            }

            return (
              <Tooltip key={constraint}>
                <TooltipTrigger render={pill} />
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
