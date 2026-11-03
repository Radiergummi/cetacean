import MetricsPanel from "./MetricsPanel";
import type { TimelineItem } from "@/lib/timeline";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";

// Mock TimeSeriesChart since it uses Chart.js, which needs a real DOM
vi.mock("./TimeSeriesChart", async () => {
  const { useMetricsPanelContext } = await import("./MetricsPanelContext");

  return {
    default: function ChartStub({ title, range }: { title: string; range: string }) {
      const panel = useMetricsPanelContext();

      return (
        <div
          data-testid={`chart-${title}`}
          data-annotating={String(panel?.annotating ?? false)}
          data-notes={panel?.timeline
            ?.filter(({ kind }) => kind === "annotation")
            .map(({ entry }) => entry.text)
            .join(",")}
        >
          {title} ({range})
        </div>
      );
    },
  };
});

const timelineNote = (id: number, tags: string[]): TimelineItem => ({
  shape: "point",
  kind: "annotation",
  at: 1_000 + id,
  entry: {
    id,
    kind: "annotation",
    timestamp: "2026-11-03T12:00:00Z",
    type: "",
    action: "",
    resourceId: "",
    name: "",
    text: `note ${id}`,
    tags,
  },
});

vi.mock("./useTimeline", () => ({
  useTimeline: () => ({
    items: [timelineNote(1, ["deploy"]), timelineNote(2, ["db"])],
    truncated: false,
  }),
}));

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

describe("MetricsPanel", () => {
  const charts = [
    { title: "CPU", query: "cpu_query", unit: "%" },
    { title: "Memory", query: "mem_query", unit: "bytes" },
  ];

  it("renders all charts", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    expect(screen.getByTestId("chart-CPU")).toBeInTheDocument();
    expect(screen.getByTestId("chart-Memory")).toBeInTheDocument();
  });

  it("renders range buttons", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    expect(screen.getByText("1H")).toBeInTheDocument();
    expect(screen.getByText("6H")).toBeInTheDocument();
    expect(screen.getByText("24H")).toBeInTheDocument();
    expect(screen.getByText("7D")).toBeInTheDocument();
  });

  it("changes range on button click", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    fireEvent.click(screen.getByText("6H"));
    expect(screen.getByText("CPU (6h)")).toBeInTheDocument();
  });

  it("shows, hides and narrows the notes by the URL and the notes menu", async () => {
    const user = userEvent.setup();
    const at = (url: string) =>
      render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={[url]}>
            <MetricsPanel
              charts={charts}
              timeline={{ type: "service", resourceId: "svc1" }}
            />
          </MemoryRouter>
        </QueryClientProvider>,
      );
    const notes = () => screen.getByTestId("chart-CPU").getAttribute("data-notes");

    const { unmount } = at("/?annotationTag=db");

    expect(notes()).toBe("note 2");
    expect(screen.getByRole("button", { name: "Annotations, filtered" })).toBeInTheDocument();
    unmount();

    at("/");
    expect(notes()).toBe("note 1,note 2");

    await user.click(screen.getByRole("button", { name: "Annotations" }));
    await user.click(await screen.findByRole("menuitemcheckbox", { name: "#deploy" }));
    expect(notes()).toBe("note 1");

    await user.click(screen.getByRole("menuitemcheckbox", { name: "Show annotations" }));
    expect(notes()).toBe("");
    expect(screen.getByRole("menuitemcheckbox", { name: "#deploy" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("offers the notes menu only where the charts draw a timeline", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });

    expect(screen.queryByRole("button", { name: "Annotations" })).not.toBeInTheDocument();
  });

  it("renders refresh button", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    expect(screen.getByTitle("Refresh")).toBeInTheDocument();
  });

  it("renders streaming toggle", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    expect(screen.getByTitle("Pause live streaming")).toBeInTheDocument();
  });

  it("names the custom range trigger", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    expect(screen.getByRole("button", { name: "Custom range" })).toBeInTheDocument();
  });

  it("offers Annotate only with a scope", () => {
    const { unmount } = render(<MetricsPanel charts={charts} />, { wrapper });

    expect(screen.queryByRole("button", { name: "Annotate" })).not.toBeInTheDocument();
    unmount();

    render(
      <MetricsPanel
        charts={charts}
        annotate={{ type: "service", id: "svc1", name: "web" }}
      />,
      { wrapper },
    );

    expect(screen.getByRole("button", { name: "Annotate" })).toBeInTheDocument();
  });

  it("leaves annotate mode on Escape", () => {
    render(
      <MetricsPanel
        charts={charts}
        annotate={{ type: "service", id: "svc1", name: "web" }}
      />,
      { wrapper },
    );

    const toggle = screen.getByRole("button", { name: "Annotate" });

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-pressed", "true");

    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(toggle).not.toHaveAttribute("aria-pressed");
  });

  it.each(["Meta", "Control"])("annotates while %s is held", (key) => {
    render(
      <MetricsPanel
        charts={charts}
        annotate={{ type: "service", id: "svc1", name: "web" }}
      />,
      { wrapper },
    );

    const chart = screen.getByTestId("chart-CPU");

    fireEvent.keyDown(document.body, { key });
    expect(chart).toHaveAttribute("data-annotating", "true");

    fireEvent.keyUp(document.body, { key });
    expect(chart).toHaveAttribute("data-annotating", "false");
  });

  it("keeps its key listeners across renders that pass an equal resource", () => {
    const listen = vi.spyOn(document, "addEventListener");
    const panel = () => (
      <MetricsPanel
        charts={charts}
        annotate={{ type: "service", id: "svc1", name: "web" }}
      />
    );
    const { rerender } = render(panel(), { wrapper });
    const keydowns = () => listen.mock.calls.filter(([type]) => type === "keydown").length;
    const before = keydowns();

    rerender(panel());

    expect(keydowns()).toBe(before);
    listen.mockRestore();
  });

  it("ignores the modifier where nothing may be annotated", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });

    fireEvent.keyDown(document.body, { key: "Meta" });

    expect(screen.getByTestId("chart-CPU")).toHaveAttribute("data-annotating", "false");
  });
});
