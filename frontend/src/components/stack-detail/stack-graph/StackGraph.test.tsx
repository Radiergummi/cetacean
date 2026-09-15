import StackGraph from "./StackGraph";
import type { StackDetail } from "@/api/types";
import type { TaskCount } from "@/lib/stackGraph";
import { fireEvent, render } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { describe, expect, it } from "vitest";

function Url() {
  return <output>{useLocation().search}</output>;
}

function draw(
  detail: StackDetail,
  { taskCounts = {}, entry = "/" }: { taskCounts?: Record<string, TaskCount>; entry?: string } = {},
) {
  const { container } = render(
    <MemoryRouter initialEntries={[entry]}>
      <StackGraph
        stack={detail}
        taskCounts={taskCounts}
      />
      <Url />
    </MemoryRouter>,
  );

  return { container, url: () => container.querySelector("output")?.textContent };
}

const stack = {
  name: "shop",
  services: [
    {
      ID: "svc-web",
      Spec: {
        Name: "shop_web",
        TaskTemplate: {
          ContainerSpec: { Image: "nginx:1.27@sha256:abc" },
          Networks: [{ Target: "net-backend" }],
        },
        Mode: { Replicated: { Replicas: 1 } },
      },
    },
  ],
  networks: [{ Id: "net-backend", Name: "shop_backend", Driver: "overlay", Scope: "swarm" }],
  configs: [],
  secrets: [],
  volumes: [{ Name: "shop_data", Driver: "local", Scope: "local" }],
} as unknown as StackDetail;

describe("StackGraph", () => {
  it("draws a node per resource, named without the stack prefix", () => {
    const { container } = draw(stack);

    const labels = [
      ["network:net-backend", "backend"],
      ["service:svc-web", "web"],
      ["volume:shop_data", "data"],
    ];

    for (const [id, label] of labels) {
      expect(container.querySelector(`.react-flow__node[data-id="${id}"]`)?.textContent).toContain(
        label,
      );
    }

    expect(container.querySelectorAll(".react-flow__node")).toHaveLength(labels.length);
  });

  it("makes every node a link to the resource it stands for", () => {
    const { container } = draw(stack);

    const links = [...container.querySelectorAll(".react-flow__node a")].map((a) =>
      a.getAttribute("href"),
    );

    expect(links.sort()).toEqual([
      "/networks/net-backend",
      "/services/svc-web",
      "/volumes/shop_data",
    ]);
  });

  it("counts running tasks against desired once the page has polled them", () => {
    const { container } = draw(stack, { taskCounts: { "svc-web": { running: 1, desired: 2 } } });
    const service = container.querySelector('.react-flow__node[data-id="service:svc-web"]');

    expect(service?.textContent).toContain("1/2 running");
    expect(service?.querySelector("[data-healthy]")).toBeNull();
  });

  it("dims what the focused node does not touch, and names it in the URL", () => {
    const { container, url } = draw(stack);
    const node = (id: string) => container.querySelector(`.react-flow__node[data-id="${id}"]`)!;

    fireEvent.focus(node("service:svc-web").querySelector("a")!);

    // The service attaches to the network; nothing mounts the volume.
    expect(node("network:net-backend").className).not.toContain("opacity-15");
    expect(node("volume:shop_data").className).toContain("opacity-15");
    expect(url()).toBe("?node=service%3Asvc-web");
  });

  it("highlights the node a link named, before anything is touched", () => {
    const { container } = draw(stack, { entry: "/?node=volume:shop_data" });
    const node = (id: string) => container.querySelector(`.react-flow__node[data-id="${id}"]`)!;

    // Nothing mounts the volume, so everything else is a step too far.
    expect(node("volume:shop_data").className).not.toContain("opacity-15");
    expect(node("service:svc-web").className).toContain("opacity-15");
    expect(node("network:net-backend").className).toContain("opacity-15");
  });

  // Edges are not here to check: jsdom lays nothing out, so React Flow never
  // gets the node sizes an edge needs and renders none.
  it("gives each node one tab stop, and React Flow's own wrapper none", () => {
    const { container } = draw(stack);
    const wrappers = [...container.querySelectorAll(".react-flow__node")];

    expect(wrappers.map((node) => node.querySelector("a")?.ariaLabel).sort()).toEqual([
      "Network backend",
      "Service web",
      "Volume data",
    ]);

    expect(wrappers.filter((node) => node.hasAttribute("tabindex"))).toEqual([]);
  });

  it("shows a service's image without its digest, and counts one replica singular", () => {
    const { container } = draw(stack);
    const service = container.querySelector('.react-flow__node[data-id="service:svc-web"]');

    expect(service?.textContent).toContain("nginx:1.27");
    expect(service?.textContent).not.toContain("sha256");
    expect(service?.textContent).toContain("1 replica");
    expect(service?.textContent).not.toContain("1 replicas");
  });
});
