import { type Gesture, step } from "./annotateGesture";
import { describe, expect, it } from "vitest";

const idle: Gesture = { kind: "idle" };

function run(events: Parameters<typeof step>[1][]) {
  let gesture = idle;
  let mark: ReturnType<typeof step>["mark"];

  for (const event of events) {
    ({ gesture, mark } = step(gesture, event));
  }

  return { gesture, mark };
}

describe("annotate gestures", () => {
  it("marks a point on a click", () => {
    expect(
      run([
        { type: "down", x: 100, touch: false },
        { type: "up", x: 102, touch: false },
      ]).mark,
    ).toEqual({ from: 100 });
  });

  it("marks a range on a drag, whichever way it goes", () => {
    expect(
      run([
        { type: "down", x: 300, touch: false },
        { type: "move", x: 200 },
        { type: "up", x: 100, touch: false },
      ]).mark,
    ).toEqual({ from: 100, to: 300 });
  });

  it("follows a drag while it lasts", () => {
    expect(
      run([
        { type: "down", x: 100, touch: false },
        { type: "move", x: 180 },
      ]).gesture,
    ).toEqual({ kind: "dragging", start: 100, current: 180 });
  });

  it("makes a range of two taps", () => {
    const first = run([
      { type: "down", x: 250, touch: true },
      { type: "up", x: 250, touch: true },
    ]);

    expect(first.mark).toBeUndefined();
    expect(first.gesture).toEqual({ kind: "tapped", at: 250 });

    expect(
      run([
        { type: "down", x: 250, touch: true },
        { type: "up", x: 250, touch: true },
        { type: "down", x: 120, touch: true },
        { type: "up", x: 120, touch: true },
      ]).mark,
    ).toEqual({ from: 120, to: 250 });
  });

  it("makes a point of two taps in one place", () => {
    expect(
      run([
        { type: "down", x: 250, touch: true },
        { type: "up", x: 250, touch: true },
        { type: "down", x: 252, touch: true },
        { type: "up", x: 252, touch: true },
      ]).mark,
    ).toEqual({ from: 250 });
  });

  it("takes a swipe on touch for scrolling, not a tap", () => {
    expect(
      run([
        { type: "down", x: 250, touch: true },
        { type: "up", x: 250, touch: true },
        { type: "down", x: 120, touch: true },
        { type: "up", x: 60, touch: true },
      ]),
    ).toEqual({ gesture: { kind: "tapped", at: 250 }, mark: undefined });
    expect(
      run([
        { type: "down", x: 100, touch: true },
        { type: "up", x: 200, touch: true },
      ]).gesture,
    ).toEqual(idle);
  });

  it("forgets a gesture on cancel", () => {
    expect(
      run([
        { type: "down", x: 100, touch: false },
        { type: "cancel" },
        { type: "up", x: 300, touch: false },
      ]),
    ).toEqual({ gesture: idle, mark: undefined });
  });
});
