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
        kind: "change",
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

  it("names who made a change through Cetacean", () => {
    const entries: HistoryEntry[] = [
      {
        id: 2,
        kind: "change",
        timestamp: new Date().toISOString(),
        type: "service",
        action: "update",
        resourceId: "svc1",
        name: "web-app",
        actor: {
          subject: "u-1",
          name: "alice",
          provider: "oauth",
          client: "claude",
          transport: "mcp",
        },
      },
      {
        id: 1,
        kind: "change",
        timestamp: new Date().toISOString(),
        type: "service",
        action: "update",
        resourceId: "svc2",
        name: "api",
        actor: { subject: "u-2", provider: "headers", transport: "dashboard" },
      },
    ];

    render(
      <MemoryRouter>
        <ActivityFeed entries={entries} />
      </MemoryRouter>,
    );

    expect(screen.getByText("by alice via MCP")).toHaveAttribute(
      "title",
      "u-1 (oauth, client claude)",
    );
    expect(screen.getByText("by u-2 via the dashboard")).toBeInTheDocument();
  });

  it("names nobody for a change made elsewhere", () => {
    const entries: HistoryEntry[] = [
      {
        id: 1,
        kind: "change",
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

    expect(screen.queryByText(/^by /)).not.toBeInTheDocument();
  });

  it("draws a gap as a divider, not as a change", () => {
    const entries: HistoryEntry[] = [
      {
        id: 3,
        kind: "change",
        timestamp: "2026-10-30T17:00:00Z",
        type: "service",
        action: "update",
        resourceId: "svc1",
        name: "web-app",
      },
      {
        id: 2,
        kind: "gap",
        timestamp: "2026-10-30T13:00:00Z",
        until: "2026-10-30T16:00:00Z",
        type: "",
        action: "",
        resourceId: "",
        name: "",
      },
    ];

    render(
      <MemoryRouter>
        <ActivityFeed entries={entries} />
      </MemoryRouter>,
    );

    const divider = screen.getByRole("separator");

    expect(divider).toHaveTextContent("Not watching for 3h");
    expect(divider).toHaveAccessibleName(/^Nothing recorded from .+ to .+$/);
    expect(screen.queryByText("d")).not.toBeInTheDocument();
    expect(screen.getByText("updated")).toBeInTheDocument();
  });

  it("skips kinds it does not know", () => {
    const entries: HistoryEntry[] = [
      {
        id: 1,
        kind: "annotation",
        timestamp: new Date().toISOString(),
        type: "",
        action: "",
        resourceId: "",
        name: "deploy freeze",
      },
    ];

    render(<ActivityFeed entries={entries} />);

    expect(screen.queryByText("deploy freeze")).not.toBeInTheDocument();
    expect(screen.getByText("No recent activity")).toBeInTheDocument();
  });
});
