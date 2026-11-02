/**
 * How far apart, in pixels, two positions must be to make a range.
 */
const rangeThreshold = 5;

/**
 * A gesture in annotate mode, in pixels: nothing yet, a mouse drag under way,
 * or the first of two taps.
 */
export type Gesture =
  | { kind: "idle" }
  | { kind: "dragging"; start: number; current: number }
  | { kind: "tapped"; at: number };

export type GestureEvent =
  | { type: "down"; x: number; touch: boolean }
  | { type: "move"; x: number }
  | { type: "up"; x: number; touch: boolean }
  | { type: "cancel" };

export interface Mark {
  from: number;
  to?: number | undefined;
}

const idle: Gesture = { kind: "idle" };

function mark(one: number, other: number): Mark {
  return Math.abs(other - one) < rangeThreshold
    ? { from: one }
    : { from: Math.min(one, other), to: Math.max(one, other) };
}

/**
 * Advances a gesture. A mouse click marks a point and a drag a range; on
 * touch, which has no hover to drag with, two taps make the range.
 */
export function step(gesture: Gesture, event: GestureEvent): { gesture: Gesture; mark?: Mark } {
  switch (event.type) {
    case "cancel":
      return { gesture: idle };

    case "down":
      if (event.touch) {
        return { gesture };
      }

      return { gesture: { kind: "dragging", start: event.x, current: event.x } };

    case "move":
      return gesture.kind === "dragging"
        ? { gesture: { ...gesture, current: event.x } }
        : { gesture };

    case "up":
      if (event.touch) {
        return gesture.kind === "tapped"
          ? { gesture: idle, mark: mark(gesture.at, event.x) }
          : { gesture: { kind: "tapped", at: event.x } };
      }

      return gesture.kind === "dragging"
        ? { gesture: idle, mark: mark(gesture.start, event.x) }
        : { gesture };
  }
}
