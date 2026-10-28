import { addressTone } from "./networkAddresses";
import { describe, expect, it } from "vitest";

function addresses(used: number, usable: number) {
  return {
    usable,
    used,
    vips: 0,
    taskAddresses: used,
    loadBalancers: 0,
    excludesAttachedContainers: false,
  };
}

describe("addressTone", () => {
  it.each([
    [79, 100, null],
    [80, 100, "warning"],
    [240, 253, "warning"],
    [95, 100, "danger"],
    [7, 5, "danger"],
    [0, 0, null],
  ])("%i of %i is %s", (used, usable, tone) => {
    expect(addressTone(addresses(used, usable))).toBe(tone);
  });
});
