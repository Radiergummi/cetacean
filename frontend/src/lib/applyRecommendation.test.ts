import { fixTarget } from "./applyRecommendation";
import type { Recommendation } from "@/api/types";
import { describe, expect, it } from "vitest";

describe("fixTarget", () => {
  it("reads the method and the target path from the fix action", () => {
    expect(
      fixTarget({ fixAction: "PUT /services/{id}/scale", targetId: "svc1" } as Recommendation),
    ).toEqual({ method: "PUT", path: "/services/svc1" });
    expect(
      fixTarget({
        fixAction: "PATCH /services/{id}/resources",
        targetId: "svc1",
      } as Recommendation),
    ).toEqual({ method: "PATCH", path: "/services/svc1" });
    expect(
      fixTarget({ fixAction: "PUT /nodes/{id}/availability", targetId: "n1" } as Recommendation),
    ).toEqual({ method: "PUT", path: "/nodes/n1" });
  });

  it("has nothing to offer without a fix action", () => {
    expect(fixTarget({ targetId: "svc1" } as Recommendation)).toBeNull();
  });
});
