import TraefikGraph from "./TraefikGraph";
import type { TraefikIntegration } from "@/api/types";
import { render } from "@testing-library/react";
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

describe("TraefikGraph", () => {
  it("draws a node for every entrypoint, router, middleware and service", () => {
    const { container } = render(<TraefikGraph integration={integration} />);

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

  it("draws a referenced but undeclared middleware as external", () => {
    const { container } = render(<TraefikGraph integration={integration} />);
    const external = container.querySelector('.react-flow__node[data-id="middleware:auth@file"]');
    const declared = container.querySelector('.react-flow__node[data-id="middleware:compress"]');

    expect(external?.querySelector(".border-dashed")).not.toBeNull();
    expect(declared?.querySelector(".border-dashed")).toBeNull();
  });
});
