import StackGraph from "./StackGraph";
import type { StackDetail } from "@/api/types";
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

function draw(detail: StackDetail) {
  return render(
    <MemoryRouter>
      <StackGraph stack={detail} />
    </MemoryRouter>,
  );
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

  it("shows a service's image without its digest, and counts one replica singular", () => {
    const { container } = draw(stack);
    const service = container.querySelector('.react-flow__node[data-id="service:svc-web"]');

    expect(service?.textContent).toContain("nginx:1.27");
    expect(service?.textContent).not.toContain("sha256");
    expect(service?.textContent).toContain("1 replica");
    expect(service?.textContent).not.toContain("1 replicas");
  });
});
