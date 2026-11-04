import type { HistoryEntry } from "../api/types";
import { formatDateTime } from "../lib/format";
import { describeAnnotationTime } from "../lib/timeline";
import { createTestQueryClient, createWrapper } from "../test/mocks";
import ActivityFeed from "./ActivityFeed";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/api/client")>();

  return {
    ...original,
    api: {
      ...original.api,
      annotation: () =>
        Promise.resolve({
          data: { id: 7, timestamp: "2026-11-01T14:00:00Z", text: "x", tags: [], resources: [] },
          allowedMethods: new Set(["GET", "PATCH"]),
          etag: '"1"',
        }),
      annotationTagsPage: () => Promise.resolve({ items: [] }),
      monitoringStatus: () =>
        Promise.resolve({ prometheusConfigured: true, prometheusReachable: true, queryable: true }),
    },
  };
});

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
      {
        id: 1,
        kind: "change",
        timestamp: "2026-10-30T12:00:00Z",
        type: "service",
        action: "create",
        resourceId: "svc1",
        name: "web-app",
      },
    ];

    render(
      <MemoryRouter>
        <ActivityFeed entries={entries} />
      </MemoryRouter>,
    );

    const divider = screen.getByText(/^Not watching for 3h/);

    expect(divider).toHaveTextContent(/^Not watching for 3h: Nothing recorded from .+ to .+$/);
    expect(divider).toHaveAttribute("title", expect.stringMatching(/^Nothing recorded from /));
    expect(screen.queryByText("d")).not.toBeInTheDocument();
    expect(screen.getByText("updated")).toBeInTheDocument();
  });

  it("says what a gap recovered, and marks a recovered change", () => {
    const entries: HistoryEntry[] = [
      {
        id: 2,
        kind: "gap",
        timestamp: "2026-10-30T13:00:00Z",
        until: "2026-10-30T16:00:00Z",
        recoveredFrom: "2026-10-30T15:00:00Z",
        type: "",
        action: "",
        resourceId: "",
        name: "",
      },
      {
        id: 1,
        kind: "change",
        timestamp: "2026-10-30T15:30:00Z",
        type: "service",
        action: "update",
        resourceId: "svc1",
        name: "web-app",
        recovered: true,
      },
    ];

    render(
      <MemoryRouter>
        <ActivityFeed entries={entries} />
      </MemoryRouter>,
    );

    const divider = screen.getByText(/^Not watching for 3h, partly covered by Docker's log/);

    expect(divider).toHaveAttribute(
      "title",
      expect.stringContaining(
        `Docker's event log covers it from ${formatDateTime("2026-10-30T15:00:00Z")}`,
      ),
    );
    expect(screen.getByText("recovered")).toHaveAttribute(
      "title",
      expect.stringMatching(/^Recovered from Docker's event log/),
    );
  });

  it("keeps a recovered change out of a watched change's run", () => {
    const change = (id: number, recovered: boolean): HistoryEntry => ({
      id,
      kind: "change",
      timestamp: `2026-10-30T1${id}:00:00Z`,
      type: "service",
      action: "update",
      resourceId: "svc1",
      name: "web-app",
      recovered,
    });

    render(
      <MemoryRouter>
        <ActivityFeed
          entries={[change(2, true), change(1, false)]}
          collapse
        />
      </MemoryRouter>,
    );

    expect(screen.queryByText("2 changes")).not.toBeInTheDocument();
    expect(screen.getAllByText("recovered")).toHaveLength(1);
  });

  it("skips kinds it does not know", () => {
    const entries: HistoryEntry[] = [
      {
        id: 1,
        kind: "note",
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

  it("says there is nothing when only gaps match", () => {
    const entries: HistoryEntry[] = [
      {
        id: 1,
        kind: "gap",
        timestamp: "2026-10-30T13:00:00Z",
        until: "2026-10-30T16:00:00Z",
        type: "",
        action: "",
        resourceId: "",
        name: "",
      },
    ];

    render(<ActivityFeed entries={entries} />);

    expect(screen.queryByText(/Not watching/)).not.toBeInTheDocument();
    expect(screen.getByText("No recent activity")).toBeInTheDocument();
  });

  it("merges gaps no change separates into one divider", () => {
    const gapEntry = (id: number, start: string, end: string): HistoryEntry => ({
      id,
      kind: "gap",
      timestamp: start,
      until: end,
      type: "",
      action: "",
      resourceId: "",
      name: "",
    });

    render(
      <MemoryRouter>
        <ActivityFeed
          entries={[
            gapEntry(3, "2026-10-30T15:00:00Z", "2026-10-30T16:00:00Z"),
            gapEntry(2, "2026-10-30T13:00:00Z", "2026-10-30T14:00:00Z"),
            {
              id: 1,
              kind: "change",
              timestamp: "2026-10-30T12:00:00Z",
              type: "service",
              action: "create",
              resourceId: "svc1",
              name: "web-app",
            },
          ]}
        />
      </MemoryRouter>,
    );

    const dividers = screen.getAllByText(/Not watching/);

    expect(dividers).toHaveLength(1);
    expect(dividers[0]).toHaveTextContent(
      /^Not watching 2 times, 2h in total: 2 stretches between .+ and .+$/,
    );
  });
});

describe("ActivityFeed runs", () => {
  const update = (id: number, overrides: Partial<HistoryEntry> = {}): HistoryEntry => ({
    id,
    kind: "change",
    timestamp: `2026-10-30T12:0${id}:00Z`,
    type: "task",
    action: "update",
    resourceId: "task1",
    name: "monitoring/prometheus.2",
    ...overrides,
  });

  const renderFeed = (entries: HistoryEntry[], collapse?: boolean) =>
    render(
      <MemoryRouter>
        <ActivityFeed
          entries={entries}
          collapse={collapse}
        />
      </MemoryRouter>,
    );

  it("collapses repeated changes into one row with a count that expands", () => {
    renderFeed([update(3), update(2), update(1)]);

    expect(screen.getAllByText("monitoring/prometheus.2")).toHaveLength(1);

    const toggle = screen.getByRole("button", { name: "3 changes" });

    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("listitem")).not.toBeInTheDocument();

    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
  });

  it("keeps an open run open as changes join it and fall off its end", () => {
    const { rerender } = renderFeed([update(3), update(2), update(1)]);

    fireEvent.click(screen.getByRole("button", { name: "3 changes" }));

    rerender(
      <MemoryRouter>
        <ActivityFeed entries={[update(4), update(3), update(2)]} />
      </MemoryRouter>,
    );

    expect(screen.getByRole("button", { name: "3 changes" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
  });

  it("folds a slot's restarts and links each to its own task", () => {
    renderFeed([update(2, { resourceId: "task-new" }), update(1, { resourceId: "task-old" })]);

    fireEvent.click(screen.getByRole("button", { name: "2 changes" }));

    const links = screen
      .getAllByRole("listitem")
      .map((item) => item.querySelector("a")?.getAttribute("href"));

    expect(links).toEqual(["/tasks/task-new", "/tasks/task-old"]);
  });

  it("lists what each folded change did", () => {
    renderFeed([update(3), update(2), update(1, { action: "create" })]);

    fireEvent.click(screen.getByRole("button", { name: "3 changes" }));

    expect(screen.getAllByRole("listitem").map(({ textContent }) => textContent?.trim())).toEqual([
      expect.stringMatching(/updated$/),
      expect.stringMatching(/updated$/),
      expect.stringMatching(/created$/),
    ]);
  });

  it("ends a run at another actor", () => {
    renderFeed([
      update(2, { actor: { subject: "u-1", provider: "oauth", transport: "mcp" } }),
      update(1),
    ]);

    expect(screen.getAllByText("monitoring/prometheus.2")).toHaveLength(2);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("ends a run at a gap", () => {
    renderFeed([
      update(3),
      {
        id: 2,
        kind: "gap",
        timestamp: "2026-10-30T11:00:00Z",
        until: "2026-10-30T11:30:00Z",
        type: "",
        action: "",
        resourceId: "",
        name: "",
      },
      update(1),
    ]);

    expect(screen.getAllByText("monitoring/prometheus.2")).toHaveLength(2);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("lists every change when not collapsing", () => {
    renderFeed([update(2), update(1)], false);

    expect(screen.getAllByText("monitoring/prometheus.2")).toHaveLength(2);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  describe("annotations", () => {
    const timestamp = "2026-11-01T14:00:00Z";
    const note = (overrides: Partial<HistoryEntry>): HistoryEntry => ({
      id: 7,
      kind: "annotation",
      timestamp,
      type: "",
      action: "",
      resourceId: "",
      name: "",
      text: "Load test\nagainst checkout",
      tags: ["load-test"],
      actor: { subject: "u1", name: "alice", provider: "oidc", transport: "dashboard" },
      ...overrides,
    });
    const show = (entry: HistoryEntry, hideType?: boolean) =>
      render(
        <ActivityFeed
          entries={[entry]}
          hideType={hideType}
        />,
        { wrapper: createWrapper(createTestQueryClient()) },
      );

    it("renders an annotation as a note card", () => {
      show(note({ resources: [{ type: "service", id: "svc1", name: "web" }] }));

      expect(
        screen.getByText(
          (_, element) =>
            element?.tagName === "P" && element.textContent === "Load test\nagainst checkout",
        ),
      ).toBeInTheDocument();
      expect(screen.getByText("#load-test")).toBeInTheDocument();
      expect(screen.getByText("by alice via the dashboard")).toBeInTheDocument();
      expect(
        screen.getByText(describeAnnotationTime(new Date(timestamp).getTime() / 1000)),
      ).toBeInTheDocument();
      expect(screen.getByRole("link", { name: "web" })).toHaveAttribute("href", "/services/svc1");
    });

    it("links the URLs in a note and opens its resource's chart around it", async () => {
      show(
        note({
          text: "See https://runbooks.example/db.",
          resources: [{ type: "service", id: "svc1", name: "web" }],
        }),
      );

      expect(screen.getByRole("link", { name: "https://runbooks.example/db" })).toHaveAttribute(
        "target",
        "_blank",
      );

      const at = new Date(timestamp).getTime() / 1000;

      expect(await screen.findByRole("link", { name: "Show on chart" })).toHaveAttribute(
        "href",
        `/services/svc1?from=${at - 1800}&to=${at + 1800}#metrics`,
      );
    });

    it("names a stacked resource as the change rows do", () => {
      show(note({ resources: [{ type: "service", id: "svc1", name: "webshop_frontend" }] }));

      const link = screen.getByRole("link", { name: /frontend/ });

      expect(link).toHaveAttribute("href", "/services/svc1");
      expect(link.textContent).not.toContain("webshop_frontend");
    });

    it("marks an edited annotation", () => {
      const updated = "2026-11-01T15:00:00Z";

      show(
        note({
          updated,
          editor: { subject: "u2", name: "bob", provider: "oidc", transport: "rest" },
        }),
      );

      expect(screen.getByText("edited")).toHaveAttribute(
        "title",
        `Edited by bob via the API ${formatDateTime(updated)}`,
      );
    });

    it("says when an annotation is cluster-wide", () => {
      const { container, unmount } = show(note({}));

      expect(screen.getByText("Cluster-wide")).toBeInTheDocument();
      expect(container.querySelector("[data-cluster-wide]")).not.toBeNull();
      unmount();

      show(note({ resources: [{ type: "service", id: "svc1", name: "web" }] }), true);

      expect(screen.queryByText("Cluster-wide")).not.toBeInTheDocument();
      expect(screen.getByRole("link", { name: "web" })).toBeInTheDocument();
    });

    it("holds its rows while a note's menu is open", async () => {
      const user = userEvent.setup();
      const feed = (entries: HistoryEntry[]) => <ActivityFeed entries={entries} />;
      const { rerender } = render(feed([note({})]), {
        wrapper: createWrapper(createTestQueryClient()),
      });

      await user.click(screen.getByRole("button", { name: "Annotation actions" }));
      await screen.findByRole("menu");
      rerender(feed([note({ id: 8, text: "Newer note" }), note({})]));

      expect(screen.queryByText("Newer note")).not.toBeInTheDocument();
      expect(screen.getByRole("status")).toHaveTextContent("Updates paused");

      await user.keyboard("{Escape}");

      expect(await screen.findByText("Newer note")).toBeInTheDocument();
      expect(screen.getByRole("status")).toBeEmptyDOMElement();
    });

    it("keeps holding when the menu hands over to a dialog", async () => {
      const user = userEvent.setup();
      const feed = (entries: HistoryEntry[]) => <ActivityFeed entries={entries} />;
      const { rerender } = render(feed([note({})]), {
        wrapper: createWrapper(createTestQueryClient()),
      });

      await user.click(screen.getByRole("button", { name: "Annotation actions" }));
      await user.click(await screen.findByRole("menuitem", { name: "Edit" }));
      await screen.findByRole("dialog");
      rerender(feed([note({ id: 8, text: "Newer note" }), note({})]));

      expect(screen.queryByText("Newer note")).not.toBeInTheDocument();
      expect(screen.getByRole("dialog")).toBeInTheDocument();
    });

    it("keeps a note's scope where the feed hides the type", () => {
      show(note({}), true);

      expect(screen.getByText("Cluster-wide")).toBeInTheDocument();
    });
  });
});
