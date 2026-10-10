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

// Each fix is offered by the method it needs: a scale is a PUT, which level 1
// allows, while the resources editor's PATCH waits for level 2.
describe("SizingBanner", () => {
  it("offers a scale fix when PUT is allowed, without PATCH", () => {
    render(
      <SizingBanner
        hints={[scaleHint]}
        allowedMethods={new Set(["GET", "HEAD", "PUT", "POST"])}
      />,
    );

    expect(screen.getByRole("button", { name: /Apply suggested value/ })).toBeInTheDocument();
  });

  it("withholds it when PUT is not allowed", () => {
    render(
      <SizingBanner
        hints={[scaleHint]}
        allowedMethods={new Set(["GET", "HEAD", "PATCH"])}
      />,
    );

    expect(screen.queryByRole("button", { name: /Apply suggested value/ })).not.toBeInTheDocument();
  });
});
