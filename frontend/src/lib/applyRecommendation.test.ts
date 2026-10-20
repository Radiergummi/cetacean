import { fixTarget } from "./applyRecommendation";
import type { Recommendation } from "@/api/types";
import { describe, expect, it } from "vitest";

describe("fixTarget", () => {
  it("reads the target path and the sub-resource from the fix action", () => {
    expect(
      fixTarget({ fixAction: "PUT /services/{id}/scale", targetId: "svc1" } as Recommendation),
    ).toEqual({ path: "/services/svc1", subResource: "scale" });
    expect(
      fixTarget({
        fixAction: "PATCH /services/{id}/resources",
        targetId: "svc1",
      } as Recommendation),
    ).toEqual({ path: "/services/svc1", subResource: "resources" });
    expect(
      fixTarget({ fixAction: "PUT /nodes/{id}/availability", targetId: "n1" } as Recommendation),
    ).toEqual({ path: "/nodes/n1", subResource: "availability" });
  });

  it("has nothing to offer without a fix action", () => {
    expect(fixTarget({ targetId: "svc1" } as Recommendation)).toBeNull();
  });
});
