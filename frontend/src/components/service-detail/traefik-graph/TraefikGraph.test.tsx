import TraefikGraph from "./TraefikGraph";
import type { TraefikIntegration } from "@/api/types";
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

const integration: TraefikIntegration = {
  name: "traefik",
  enabled: true,
  routers: [
    {
      name: "web",
      rule: "Host(`shop.example.com`)",
      entrypoints: ["websecure"],
      middlewares: ["auth@file"],
      service: "shop",
    },
    { name: "admin" },
  ],
  services: [
    { name: "shop", port: 8080, scheme: "http" },
    { name: "metrics", port: 9090 },
  ],
  middlewares: [{ name: "compress", type: "compress" }],
};

function draw(detail = integration) {
  return render(
    <MemoryRouter>
      <TraefikGraph integration={detail} />
    </MemoryRouter>,
  );
}

describe("TraefikGraph", () => {
  it("draws a node for every entrypoint, router, middleware and service", () => {
    const { container } = draw();

    const labels = [
      ["entrypoint:websecure", "websecure"],
      ["router:web", "web"],
      ["router:admin", "admin"],
      ["middleware:auth@file", "auth@file"],
      ["middleware:compress", "compress"],
      ["service:shop", "shop"],
      ["service:metrics", "metrics"],
      ["service:unresolved:admin", "Unresolved"],
    ];

    for (const [id, label] of labels) {
      const node = container.querySelector(`.react-flow__node[data-id="${id}"]`);

      expect(node?.textContent).toContain(label);
    }

    expect(container.querySelectorAll(".react-flow__node")).toHaveLength(labels.length);
  });

  it("puts every node that hides detail in the tab order, and nothing else", () => {
    const { container } = draw();

    const focusable = [...container.querySelectorAll(".react-flow__node button")].map((element) =>
      element.getAttribute("aria-label"),
    );

    // The name carries what the face shows, since React Flow's wrapper is a
    // `role="application"` a screen reader can only tab through.
    expect(focusable.sort()).toEqual([
      "Middleware auth@file",
      "Middleware compress",
      "Router admin",
      "Router web, Host(`shop.example.com`)",
      "Service metrics, :9090",
      "Service shop, http:8080",
      "Unresolved service",
    ]);

    // The entrypoint reveals nothing a tooltip would add.
    const wrappers = [...container.querySelectorAll(".react-flow__node")];

    expect(wrappers.filter((node) => node.hasAttribute("tabindex"))).toEqual([]);
  });

  it("draws a referenced but undeclared middleware as external", () => {
    const { container } = draw();
    const external = container.querySelector('.react-flow__node[data-id="middleware:auth@file"]');
    const declared = container.querySelector('.react-flow__node[data-id="middleware:compress"]');

    expect(external?.querySelector(".border-dashed")).not.toBeNull();
    expect(declared?.querySelector(".border-dashed")).toBeNull();
  });
});
