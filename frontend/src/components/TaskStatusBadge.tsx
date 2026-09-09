import { Badge } from "./ui/badge";
import { statusTone, toneSurface } from "@/lib/statusColor";

/**
 * A resource state, rendered as a badge.
 *
 * This was a single className carrying forty `data-[state=…]` variants over
 * hardcoded palette shades, which restated the state-to-meaning mapping that
 * `lib/statusColor` already owned. It now asks that module what a state means
 * and renders the shared Badge, so the dot beside a name and the badge below
 * it cannot disagree.
 */
export default function TaskStatusBadge({ state }: { state?: string | undefined }) {
  const label = state || "unknown";

  return (
    <Badge
      data-state={label}
      className={toneSurface[statusTone(label)]}
    >
      {label}
    </Badge>
  );
}
