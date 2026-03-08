import type { NotificationRuleStatus } from "../api/types";
import TimeAgo from "./TimeAgo";

interface NotificationRulesProps {
  rules: NotificationRuleStatus[];
}

export default function NotificationRules({ rules }: NotificationRulesProps) {
  return (
    <div className="rounded-lg border bg-card p-4">
      {rules.map((rule) => (
        <div key={rule.id} className="flex items-center gap-3 py-1 min-h-7 text-sm">
          <span
            className={`w-2 h-2 rounded-full shrink-0 ${rule.enabled ? "bg-green-500" : "bg-gray-400"}`}
          />
          <span className="font-medium truncate flex-1">{rule.name}</span>
          {rule.fireCount > 0 && (
            <span className="rounded border px-1.5 py-0.5 text-xs text-muted-foreground tabular-nums">
              {rule.fireCount}
            </span>
          )}
          {rule.lastFired && (
            <span className="text-xs text-muted-foreground whitespace-nowrap">
              <TimeAgo date={rule.lastFired} />
            </span>
          )}
        </div>
      ))}
    </div>
  );
}
