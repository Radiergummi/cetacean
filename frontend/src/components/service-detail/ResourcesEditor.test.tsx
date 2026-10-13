import { ResourcesEditor } from "./ResourcesEditor";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("ResourcesEditor", () => {
  it("shows reserved generic resources instead of the empty state", () => {
    render(
      <ResourcesEditor
        serviceId="s1"
        resources={{
          Reservations: {
            GenericResources: [
              { DiscreteResourceSpec: { Kind: "gpu", Value: 2 } },
              { NamedResourceSpec: { Kind: "fpga", Value: "f1" } },
            ],
          },
        }}
        onSaved={() => {}}
      />,
    );

    expect(screen.queryByText("No resource limits configured")).not.toBeInTheDocument();
    expect(screen.getByText("gpu=2, fpga=f1")).toBeInTheDocument();
  });
});
