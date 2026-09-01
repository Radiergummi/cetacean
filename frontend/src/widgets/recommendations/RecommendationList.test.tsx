import { RecommendationList } from "./RecommendationList";
import type { Recommendation } from "./types";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const findings: Recommendation[] = [
  {
    category: "no-healthcheck",
    severity: "warning",
    scope: "service",
    targetId: "svc1",
    targetName: "api",
    message: "no health check configured",
  },
  {
    category: "flaky-service",
    severity: "critical",
    scope: "service",
    targetId: "svc2",
    targetName: "worker",
    message: "restarting repeatedly",
  },
];

describe("RecommendationList", () => {
  it("renders one entry per finding", () => {
    render(<RecommendationList items={findings} />);

    expect(screen.getByText(/no health check configured/)).toBeInTheDocument();
    expect(screen.getByText(/restarting repeatedly/)).toBeInTheDocument();
  });

  it("groups by severity, most serious first", () => {
    render(<RecommendationList items={findings} />);

    const groups = screen.getAllByRole("group");

    expect(within(groups[0]!).getByText(/restarting repeatedly/)).toBeInTheDocument();
    expect(within(groups[1]!).getByText(/no health check configured/)).toBeInTheDocument();
  });

  // Severity is never carried by color alone: each group is labelled.
  it("labels each severity group in text", () => {
    render(<RecommendationList items={findings} />);

    expect(screen.getByText(/1 critical/i)).toBeInTheDocument();
    expect(screen.getByText(/1 warning/i)).toBeInTheDocument();
  });

  it("hands a finding back to the model when it is picked", () => {
    const onInvestigate = vi.fn<(finding: Recommendation) => void>();

    render(
      <RecommendationList
        items={findings}
        onInvestigate={onInvestigate}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /restarting repeatedly/ }));

    expect(onInvestigate).toHaveBeenCalledWith(findings[1]);
  });

  it("says so when there is nothing to report", () => {
    render(<RecommendationList items={[]} />);

    expect(screen.getByText(/nothing to report/i)).toBeInTheDocument();
  });
});
