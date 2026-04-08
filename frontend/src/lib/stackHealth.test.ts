import { stackHealth } from "./stackHealth";
import { describe, it, expect } from "vitest";

describe("stackHealth", () => {
  it("returns healthy when running matches desired", () => {
    expect(stackHealth({ running: 3 }, 3)).toBe("healthy");
  });

  it("returns healthy when running exceeds desired", () => {
    expect(stackHealth({ running: 5 }, 3)).toBe("healthy");
  });

  it("returns warning when running is below desired without failures", () => {
    expect(stackHealth({ running: 1 }, 3)).toBe("warning");
  });

  it("returns critical when running is below desired with failed tasks", () => {
    expect(stackHealth({ running: 1, failed: 2 }, 3)).toBe("critical");
  });

  it("returns critical when running is below desired with rejected tasks", () => {
    expect(stackHealth({ running: 0, rejected: 1 }, 3)).toBe("critical");
  });

  it("returns warning when zero running and no failures", () => {
    expect(stackHealth({ running: 0 }, 3)).toBe("warning");
  });

  it("handles empty tasksByState", () => {
    expect(stackHealth({}, 3)).toBe("warning");
  });

  it("returns healthy when desired is zero", () => {
    expect(stackHealth({}, 0)).toBe("healthy");
  });
});
