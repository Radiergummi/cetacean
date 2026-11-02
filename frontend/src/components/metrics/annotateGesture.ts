/**
 * How far apart, in pixels, two positions must be to make a range.
 */
const rangeThreshold = 5;

/**
 * How far, in pixels, a finger may drift between press and lift and still tap.
 */
const tapSlop = 10;

/**
 * A gesture in annotate mode, in pixels: nothing yet, a mouse drag under way,
 * or the first of two taps. `pressed` is where a touch went down, so a lift
 * far from it, a swipe, is not taken for a tap.
 */
export type Gesture =
  | { kind: "idle"; pressed?: number | undefined }
  | { kind: "dragging"; start: number; current: number }
  | { kind: "tapped"; at: number; pressed?: number | undefined };

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
        return gesture.kind === "dragging"
          ? { gesture }
          : { gesture: { ...gesture, pressed: event.x } };
      }

      return { gesture: { kind: "dragging", start: event.x, current: event.x } };

    case "move":
      return gesture.kind === "dragging"
        ? { gesture: { ...gesture, current: event.x } }
        : { gesture };

    case "up":
      if (event.touch) {
        if (gesture.kind === "dragging") {
          return { gesture };
        }

        const { pressed, ...rest } = gesture;

        if (pressed === undefined || Math.abs(event.x - pressed) > tapSlop) {
          return { gesture: rest };
        }

        return gesture.kind === "tapped"
          ? { gesture: idle, mark: mark(gesture.at, event.x) }
          : { gesture: { kind: "tapped", at: event.x } };
      }

      return gesture.kind === "dragging"
        ? { gesture: idle, mark: mark(gesture.start, event.x) }
        : { gesture };
  }
}
