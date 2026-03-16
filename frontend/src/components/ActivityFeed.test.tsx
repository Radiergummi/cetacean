import type { HistoryEntry } from "../api/types";
import ActivityFeed from "./ActivityFeed";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect } from "vitest";

describe("ActivityFeed", () => {
  it("shows loading skeleton", () => {
    const { container } = render(
      <ActivityFeed
        entries={[]}
        loading
      />,
    );
    expect(container.querySelectorAll(".bg-muted").length).toBeGreaterThan(0);
  });

  it("shows empty message when no entries", () => {
    render(<ActivityFeed entries={[]} />);
    expect(screen.getByText("No recent activity")).toBeInTheDocument();
  });

  it("renders entries", () => {
    const entries: HistoryEntry[] = [
      {
        id: 1,
        timestamp: new Date().toISOString(),
        type: "service",
        action: "update",
        resourceId: "svc1",
        name: "web-app",
      },
    ];
    render(
      <MemoryRouter>
        <ActivityFeed entries={entries} />
      </MemoryRouter>,
    );
    expect(screen.getByText("web-app")).toBeInTheDocument();
    expect(screen.getByText("updated")).toBeInTheDocument();
    expect(screen.getByText("service")).toBeInTheDocument();
  });
});
