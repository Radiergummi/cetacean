import { SizingBanner } from "./SizingBanner";
import type { Recommendation } from "@/api/types";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

const scaleHint = {
  category: "single-replica",
  severity: "warning",
  targetId: "svc1",
  message: "Only one replica",
  fixAction: "PUT /services/{id}/scale",
  suggested: 2,
} as Recommendation;

// Each fix is offered by the sub-resource it writes: a scale opens at level 1,
// while the resources editor waits for level 2.
describe("SizingBanner", () => {
  it("offers a scale fix when scale is editable, without resources", () => {
    render(
      <SizingBanner
        hints={[scaleHint]}
        editable={new Set(["scale", "restart"])}
      />,
    );

    expect(screen.getByRole("button", { name: /Apply suggested value/ })).toBeInTheDocument();
  });

  it("withholds it when scale is not editable", () => {
    render(
      <SizingBanner
        hints={[scaleHint]}
        editable={new Set(["resources"])}
      />,
    );

    expect(screen.queryByRole("button", { name: /Apply suggested value/ })).not.toBeInTheDocument();
  });
});
