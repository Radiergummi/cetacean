import TaskTimeline from "./TaskTimeline";
import type { Task } from "@/api/types";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

const minute = 60_000;
const now = Date.parse("2026-11-01T12:00:00Z");
const timeWindow = { start: now - 60 * minute, end: now };

function makeTask(id: string, createdAgo: number, state = "running"): Task {
  const createdAt = new Date(now - createdAgo * minute).toISOString();

  return {
    ID: id,
    Version: { Index: 1 },
    CreatedAt: createdAt,
    ServiceID: "service-a",
    ServiceName: "web",
    NodeID: "node-a",
    NodeHostname: "worker-1",
    Slot: 1,
    Status: {
      Timestamp:
        state === "running" ? createdAt : new Date(now - (createdAgo - 1) * minute).toISOString(),
      State: state,
    },
    DesiredState: "running",
    Spec: {},
  };
}

function LocationProbe() {
  return <output aria-label="location">{useLocation().search}</output>;
}

function currentParams() {
  return new URLSearchParams(screen.getByRole("status", { name: "location" }).textContent ?? "");
}

function renderTimeline(tasks: Task[], retentionLimit = 5) {
  return render(
    <MemoryRouter>
      <TaskTimeline
        tasks={tasks}
        variant="service"
        timeWindow={timeWindow}
        now={now}
        retentionLimit={retentionLimit}
      />
      <LocationProbe />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("TaskTimeline", () => {
  it("leaves the time before a new replica's first task plain", () => {
    renderTimeline([makeTask("aaaaaaaaaaaa1111", 5)]);

    expect(screen.queryByRole("img", { name: /may be missing/ })).toBeNull();
  });

  it("hatches a replica whose older tasks Swarm may have pruned, and says why", () => {
    renderTimeline(
      [1, 2, 3, 4, 5].map((index) => makeTask(`task${index}`, 10 - index, "failed")),
      5,
    );

    expect(
      screen.getByRole("img", {
        name: "Swarm keeps only the newest 5 tasks per replica, so older ones may be missing here.",
      }),
    ).toBeInTheDocument();
  });

  it("labels a bar with its task's short ID", () => {
    renderTimeline([makeTask("abcdefabcdef9999", 30)]);

    expect(screen.getByText("abcdefabcdef")).toBeInTheDocument();
  });

  it("zooms the page's range to a drag across the tracks", () => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
      left: 0,
      width: 600,
      top: 0,
      right: 600,
      bottom: 20,
      height: 20,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });

    const { container } = renderTimeline([makeTask("abcdefabcdef9999", 30)]);
    const track = container.querySelector(".cursor-crosshair");

    if (!track) {
      throw new Error("no track");
    }

    HTMLElement.prototype.setPointerCapture = vi.fn<(pointerId: number) => void>();

    fireEvent.pointerDown(track, { button: 0, clientX: 300 });
    fireEvent.pointerMove(track, { clientX: 450 });
    fireEvent.pointerUp(track);

    const params = currentParams();

    expect(Number(params.get("from")) * 1000).toBe(now - 30 * minute);
    expect(Number(params.get("to")) * 1000).toBe(now - 15 * minute);
  });
});
