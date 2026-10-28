import { stableBatches } from "./stableBatches";
import { describe, expect, it } from "vitest";

const ids = (count: number, prefix = "svc") =>
  Array.from({ length: count }, (_, index) => `${prefix}${index}`);

describe("stableBatches", () => {
  it("puts every ID in exactly one batch", () => {
    const batches = stableBatches(ids(130), 50);

    expect(batches.flat().sort()).toEqual(ids(130).sort());
  });

  it("uses a power-of-two number of buckets covering the IDs at the batch size", () => {
    expect(stableBatches(ids(10), 50)).toHaveLength(1);
    expect(stableBatches(ids(130), 50).length).toBeLessThanOrEqual(4);
  });

  it("is empty for no IDs", () => {
    expect(stableBatches([], 50)).toEqual([]);
  });

  it("does not depend on the order the IDs arrive in", () => {
    expect(stableBatches(ids(130).reverse(), 50)).toEqual(stableBatches(ids(130), 50));
  });

  // A service starting or stopping its last task changes one batch, so only
  // that batch's metrics query has to run again.
  it("changes only one batch when one ID joins", () => {
    const before = stableBatches(ids(90), 50);
    const after = stableBatches([...ids(90), "svc-new"], 50);
    const unchanged = after.filter((batch) =>
      before.some((previous) => previous.join() === batch.join()),
    );

    expect(after).toHaveLength(before.length);
    expect(unchanged).toHaveLength(after.length - 1);
  });
});
