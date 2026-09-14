import StackGraph from "./StackGraph";
import type { StackDetail } from "@/api/types";
import type { TaskCount } from "@/lib/stackGraph";
import { fireEvent, render } from "@testing-library/react";
import { useEffect } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { describe, expect, it } from "vitest";

function draw(detail: StackDetail, taskCounts: Record<string, TaskCount> = {}, entry = "/") {
  const seen = { search: "" };

  function Probe() {
    const { search } = useLocation();

    useEffect(() => {
      seen.search = search;
    }, [search]);

    return null;
  }

  const { container } = render(
    <MemoryRouter initialEntries={[entry]}>
      <StackGraph
        stack={detail}
        taskCounts={taskCounts}
      />
      <Probe />
    </MemoryRouter>,
  );

  return { container, url: () => seen.search };
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
    const { container } = draw(stack, { "svc-web": { running: 1, desired: 2 } });
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
    const { container } = draw(stack, {}, "/?node=volume:shop_data");
    const node = (id: string) => container.querySelector(`.react-flow__node[data-id="${id}"]`)!;

    // Nothing mounts the volume, so everything else is a step too far.
    expect(node("volume:shop_data").className).not.toContain("opacity-15");
    expect(node("service:svc-web").className).toContain("opacity-15");
    expect(node("network:net-backend").className).toContain("opacity-15");
  });

  it("leaves React Flow's own node and edge tab stops out of a read-only graph", () => {
    const { container } = draw(stack);

    expect(container.querySelectorAll(".react-flow__node[tabindex]")).toHaveLength(0);
    expect(container.querySelectorAll(".react-flow__edge[tabindex]")).toHaveLength(0);
    expect(container.querySelectorAll(".react-flow__node a")).toHaveLength(3);
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
