import { showsGraph } from "./viewport";
import { describe, expect, it } from "vitest";

const bounds = { width: 400, height: 300 };
const frame = { width: 800, height: 600 };

describe("showsGraph", () => {
  it("accepts a viewport the graph is inside", () => {
    expect(showsGraph({ x: 0, y: 0, zoom: 1 }, bounds, frame.width, frame.height)).toBe(true);
  });

  it("accepts a viewport showing one corner of the graph", () => {
    expect(showsGraph({ x: -350, y: -250, zoom: 1 }, bounds, frame.width, frame.height)).toBe(true);
  });

  it("rejects a viewport panned past the graph", () => {
    expect(showsGraph({ x: -1200, y: 0, zoom: 1 }, bounds, frame.width, frame.height)).toBe(false);
  });

  it("rejects a viewport zoomed into empty space beyond the graph", () => {
    expect(showsGraph({ x: -1000, y: -800, zoom: 2 }, bounds, frame.width, frame.height)).toBe(
      false,
    );
  });
});
