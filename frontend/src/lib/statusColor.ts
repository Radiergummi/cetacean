/**
 * The one mapping from a resource state to a meaning.
 *
 * `statusColor` and `TaskStatusBadge` used to encode this separately — one as
 * `bg-green-500`, the other as forty `data-[state=…]` variants over
 * `bg-green-100 text-green-800` and a dark counterpart. They agreed by
 * coincidence rather than by construction. Both now read from here, so a state
 * that changes meaning changes in one place.
 */
export type StatusTone = "ok" | "warning" | "danger" | "info" | "neutral";

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
  info: "bg-status-info",
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
  info: "bg-status-info/15 text-status-info",
  neutral: "bg-status-neutral/15 text-status-neutral",
};

/** Text alone, for an icon or a label sitting on the page background. */
export const toneText: Record<StatusTone, string> = {
  ok: "text-status-ok",
  warning: "text-status-warning",
  danger: "text-status-danger",
  info: "text-status-info",
  neutral: "text-status-neutral",
};

/**
 * A banner: the tinted surface plus an outline. It borrows the surface rather
 * than restating the tint, because the contrast the tokens were chosen for is
 * contrast against *that* tint.
 */
export const toneBanner: Record<StatusTone, string> = {
  ok: `${toneSurface.ok} border-status-ok/30`,
  warning: `${toneSurface.warning} border-status-warning/30`,
  danger: `${toneSurface.danger} border-status-danger/30`,
  info: `${toneSurface.info} border-status-info/30`,
  neutral: `${toneSurface.neutral} border-status-neutral/30`,
};
