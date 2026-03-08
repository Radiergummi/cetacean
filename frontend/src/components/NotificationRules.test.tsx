import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import NotificationRules from "./NotificationRules";
import type { NotificationRuleStatus } from "../api/types";

describe("NotificationRules", () => {
  it("renders rule names", () => {
    const rules: NotificationRuleStatus[] = [
      { id: "r1", name: "Node Down Alert", enabled: true, fireCount: 0 },
      { id: "r2", name: "Task Failure", enabled: false, fireCount: 3 },
    ];
    render(<NotificationRules rules={rules} />);
    expect(screen.getByText("Node Down Alert")).toBeInTheDocument();
    expect(screen.getByText("Task Failure")).toBeInTheDocument();
  });

  it("shows fire count when > 0", () => {
    const rules: NotificationRuleStatus[] = [
      { id: "r1", name: "Alert", enabled: true, fireCount: 5 },
    ];
    render(<NotificationRules rules={rules} />);
    expect(screen.getByText("5")).toBeInTheDocument();
  });

  it("hides fire count when 0", () => {
    const rules: NotificationRuleStatus[] = [
      { id: "r1", name: "Alert", enabled: true, fireCount: 0 },
    ];
    render(<NotificationRules rules={rules} />);
    expect(screen.queryByText("0")).not.toBeInTheDocument();
  });
});
