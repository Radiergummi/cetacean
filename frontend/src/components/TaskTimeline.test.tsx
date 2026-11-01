import TaskTimeline from "./TaskTimeline";
import type { Task } from "@/api/types";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
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

const onZoom = vi.fn<(from: number, to: number) => void>();

/**
 * Gives every element, the tracks included, a 600px box from x = 0.
 */
function mockTrack() {
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
  HTMLElement.prototype.setPointerCapture = vi.fn<(pointerId: number) => void>();
}

function trackOf(container: HTMLElement): Element {
  const track = container.querySelector(".cursor-crosshair");

  if (!track) {
    throw new Error("no track");
  }

  return track;
}

function renderTimeline(tasks: Task[], retentionLimit = 5, window = timeWindow) {
  return render(
    <MemoryRouter>
      <TaskTimeline
        tasks={tasks}
        variant="service"
        timeWindow={window}
        now={now}
        retentionLimit={retentionLimit}
        isCustomRange={false}
        onZoom={onZoom}
        onResetZoom={vi.fn<() => void>()}
      />
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

  it("hatches by the limit a service's restart policy sets", () => {
    renderTimeline(
      [1, 2, 3].map((index) => ({
        ...makeTask(`task${index}`, 10 - index, "failed"),
        Spec: { RestartPolicy: { MaxAttempts: 2 } },
      })),
      -1,
    );

    expect(
      screen.getByRole("img", {
        name: "Swarm keeps only the newest 3 tasks per replica, so older ones may be missing here.",
      }),
    ).toBeInTheDocument();
  });

  it("leaves rows plain where Swarm never prunes", () => {
    renderTimeline(
      [1, 2, 3, 4, 5].map((index) => makeTask(`task${index}`, 10 - index, "failed")),
      -1,
    );

    expect(screen.queryByRole("img", { name: /may be missing/ })).toBeNull();
  });

  it("labels a bar with its task's short ID", () => {
    renderTimeline([makeTask("abcdefabcdef9999", 30)]);

    expect(screen.getByText("abcdefabcdef")).toBeInTheDocument();
  });

  it("zooms the section's range to a drag across the tracks", () => {
    mockTrack();

    const { container } = renderTimeline([makeTask("abcdefabcdef9999", 30)]);
    const track = trackOf(container);

    fireEvent.pointerDown(track, { button: 0, clientX: 300 });
    fireEvent.pointerMove(track, { buttons: 1, clientX: 450 });
    fireEvent.pointerUp(track);

    expect(onZoom).toHaveBeenCalledWith((now - 30 * minute) / 1000, (now - 15 * minute) / 1000);
  });

  it("zooms to at least a whole second", () => {
    mockTrack();

    const { container } = renderTimeline([makeTask("abcdefabcdef9999", 30)], 5, {
      start: now - 2000,
      end: now,
    });
    const track = trackOf(container);

    onZoom.mockClear();
    fireEvent.pointerDown(track, { button: 0, clientX: 300 });
    fireEvent.pointerMove(track, { buttons: 1, clientX: 306 });
    fireEvent.pointerUp(track);

    const [from, to] = onZoom.mock.calls[0] ?? [];

    expect(Number.isInteger(from) && Number.isInteger(to)).toBe(true);
    expect((to ?? 0) - (from ?? 0)).toBeGreaterThanOrEqual(1);
  });

  it("drops a selection whose button was released off the track", () => {
    mockTrack();

    const { container } = renderTimeline([makeTask("abcdefabcdef9999", 30)]);
    const track = trackOf(container);

    onZoom.mockClear();
    fireEvent.pointerDown(track, { button: 0, clientX: 300 });
    fireEvent.pointerMove(track, { buttons: 0, clientX: 450 });
    fireEvent.pointerUp(track);

    expect(onZoom).not.toHaveBeenCalled();
  });
});
