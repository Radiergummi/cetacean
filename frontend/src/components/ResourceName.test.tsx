import ResourceName from "./ResourceName";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("ResourceName", () => {
  it("guesses at the first underscore when no stack is supplied", () => {
    // Search results and chart series hold no labels; the guess is the best
    // answer available there.
    render(<ResourceName name="monitoring_prometheus" />);

    expect(screen.getByText("monitoring/")).toBeInTheDocument();
    expect(screen.getByText("prometheus")).toBeInTheDocument();
  });

  it("splits on the label when one is supplied", () => {
    render(
      <ResourceName
        name="monitoring_prometheus"
        stack="monitoring"
      />,
    );

    expect(screen.getByText("monitoring/")).toBeInTheDocument();
    expect(screen.getByText("prometheus")).toBeInTheDocument();
  });

  it("never splits a name the caller knows belongs to no stack", () => {
    // The breadcrumb beside this title reads "Volumes › my_data"; splitting the
    // title into "my/data" would contradict it. An underscore in an unstacked
    // name is just an underscore.
    render(
      <ResourceName
        name="my_data"
        stack={null}
      />,
    );

    expect(screen.getByText("my_data")).toBeInTheDocument();
    expect(screen.queryByText("my/")).not.toBeInTheDocument();
  });

  it("leaves a name that only joined its stack by label alone", () => {
    // The resource is in the stack but is not named for it, so there is no
    // prefix to peel off and nothing to mute.
    render(
      <ResourceName
        name="prometheus"
        stack="monitoring"
      />,
    );

    expect(screen.getByText("prometheus")).toBeInTheDocument();
    expect(screen.queryByText("monitoring/")).not.toBeInTheDocument();
  });
});
