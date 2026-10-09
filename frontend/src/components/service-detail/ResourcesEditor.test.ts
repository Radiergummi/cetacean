import { buildResourcesPatch } from "./ResourcesEditor";
import { describe, expect, it } from "vitest";

describe("buildResourcesPatch", () => {
  it("sends a cleared limit as null so the server removes it", () => {
    expect(buildResourcesPatch({ reservation: 0.5 }, { limit: 512 })).toEqual({
      Limits: { NanoCPUs: null, MemoryBytes: 512 * 1024 * 1024 },
      Reservations: { NanoCPUs: 500_000_000, MemoryBytes: null },
    });
  });
});
