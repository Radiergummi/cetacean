import { ReplicaCard } from "./ReplicaCard";
import type { Service } from "@/api/types";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("ReplicaCard", () => {
  it("names the icon-only replica editor for assistive technology", () => {
    const service = {
      ID: "svc1",
      Spec: { Name: "web", Mode: { Replicated: { Replicas: 2 } } },
    } as unknown as Service;

    render(
      <ReplicaCard
        service={service}
        tasks={[]}
        canScale
      />,
    );

    expect(screen.getByRole("button", { name: "Edit replicas" })).toBeInTheDocument();
  });
});
