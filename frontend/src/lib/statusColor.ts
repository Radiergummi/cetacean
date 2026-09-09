/**
 * The one mapping from a resource state to a meaning.
 *
 * `statusColor` and `TaskStatusBadge` used to encode this separately — one as
 * `bg-green-500`, the other as forty `data-[state=…]` variants over
 * `bg-green-100 text-green-800` and a dark counterpart. They agreed by
 * coincidence rather than by construction. Both now read from here, so a state
 * that changes meaning changes in one place.
 */
export type StatusTone = "ok" | "warning" | "danger" | "neutral";

const stateTones: Record<string, StatusTone> = {
  running: "ok",
  ready: "ok",
  complete: "ok",
  failed: "danger",
  rejected: "danger",
  down: "danger",
  orphaned: "danger",
  preparing: "warning",
  starting: "warning",
  pending: "warning",
  assigned: "warning",
  accepted: "warning",
  shutdown: "neutral",
  remove: "neutral",
};

export function statusTone(state: string): StatusTone {
  return stateTones[state] ?? "neutral";
}

const toneDot: Record<StatusTone, string> = {
  ok: "bg-status-ok",
  warning: "bg-status-warning",
  danger: "bg-status-danger",
  neutral: "bg-status-neutral",
};

/** Solid fill for the status dot beside a resource name. */
export function statusColor(state: string): string {
  return toneDot[statusTone(state)];
}

/** Same fill, chosen from running/total rather than from a state string. */
export function replicaHealthColor(running: number, total: number): string {
  if (running === total) {
    return toneDot.ok;
  }

  if (running > 0) {
    return toneDot.warning;
  }

  return toneDot.danger;
}

/**
 * Tinted surface plus matching text, for a badge or a banner. The tint is the
 * same token at low opacity, which is how ui/button.tsx and ui/badge.tsx
 * already render their destructive variant.
 */
export const toneSurface: Record<StatusTone, string> = {
  ok: "bg-status-ok/15 text-status-ok",
  warning: "bg-status-warning/15 text-status-warning",
  danger: "bg-status-danger/15 text-status-danger",
  neutral: "bg-status-neutral/15 text-status-neutral",
};
