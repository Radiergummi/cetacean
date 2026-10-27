import PhysicalNodeCard, { type PhysicalNodeData } from "./PhysicalNodeCard";
import { PhysicalViewContext, type PhysicalViewState } from "./PhysicalViewContext";
import type { NodeMetrics } from "@/hooks/useNodeMetrics";
import { render, screen } from "@testing-library/react";
import type { NodeProps } from "@xyflow/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

const data: PhysicalNodeData = {
  label: "worker-01",
  address: "10.0.0.5",
  role: "worker",
  state: "ready",
  availability: "active",
  services: [
    {
      serviceId: "svc1",
      serviceName: "web",
      image: "nginx",
      running: 1,
      total: 1,
      states: ["running"],
      networks: ["net1"],
    },
    {
      serviceId: "svc2",
      serviceName: "cron",
      image: "alpine",
      running: 1,
      total: 1,
      states: ["running"],
      networks: [],
    },
  ],
};

const noFigures: NodeMetrics = { cpu: null, memory: null, disk: null, cpuHistory: [] };

function renderCard(state: Partial<PhysicalViewState>) {
  const props = { id: "urn:cetacean:node:n1", data } as unknown as NodeProps & {
    data: PhysicalNodeData;
  };

  return render(
    <MemoryRouter>
      <PhysicalViewContext.Provider
        value={{ network: null, usage: {}, nodeMetrics: undefined, ...state }}
      >
        <PhysicalNodeCard {...props} />
      </PhysicalViewContext.Provider>
    </MemoryRouter>,
  );
}

describe("PhysicalNodeCard", () => {
  it("shows a service's usage on this node, keyed by the node's own ID", () => {
    renderCard({ usage: { "svc1/n1": { cpu: 12.5, memory: null } } });

    expect(screen.getByRole("button", { name: "Service web" }).textContent).toContain("12.5%");
    expect(screen.getByTitle("12.5%").className).toContain("truncate");
  });

  it("shows no node figures when the node has none", () => {
    renderCard({ nodeMetrics: () => noFigures });

    expect(screen.queryByText(/CPU/)).toBeNull();
  });

  it("shows the node's figures when it has them", () => {
    renderCard({ nodeMetrics: () => ({ ...noFigures, cpu: 42 }) });

    expect(screen.getByText(/CPU/).textContent).toContain("42%");
  });

  it("dims the services off the selected network", () => {
    renderCard({ network: "net1" });

    expect(screen.getByRole("button", { name: "Service web" }).dataset["dimmed"]).toBeUndefined();
    expect(screen.getByRole("button", { name: "Service cron" }).dataset["dimmed"]).toBe("true");
  });
});
