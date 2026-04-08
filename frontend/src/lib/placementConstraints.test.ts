import { humanizeConstraint } from "./placementConstraints";
import { describe, it, expect } from "vitest";

describe("humanizeConstraint", () => {
  it("returns null for unparseable input", () => {
    expect(humanizeConstraint("garbage")).toBeNull();
    expect(humanizeConstraint("")).toBeNull();
  });

  it("parses manager role include", () => {
    expect(humanizeConstraint("node.role == manager")).toEqual({
      label: "Manager nodes only",
      exclude: false,
    });
  });

  it("parses worker role include", () => {
    expect(humanizeConstraint("node.role == worker")).toEqual({
      label: "Worker nodes only",
      exclude: false,
    });
  });

  it("parses manager role exclude", () => {
    expect(humanizeConstraint("node.role != manager")).toEqual({
      label: "Exclude manager nodes",
      exclude: true,
    });
  });

  it("parses worker role exclude", () => {
    expect(humanizeConstraint("node.role != worker")).toEqual({
      label: "Exclude worker nodes",
      exclude: true,
    });
  });

  it("parses hostname include", () => {
    expect(humanizeConstraint("node.hostname == web-01")).toEqual({
      label: "Node: web-01",
      exclude: false,
    });
  });

  it("parses hostname exclude", () => {
    expect(humanizeConstraint("node.hostname != db-01")).toEqual({
      label: "Exclude node db-01",
      exclude: true,
    });
  });

  it("parses node id", () => {
    expect(humanizeConstraint("node.id == abc123")).toEqual({
      label: "Node ID: abc123",
      exclude: false,
    });
  });

  it("parses platform os", () => {
    expect(humanizeConstraint("node.platform.os == linux")).toEqual({
      label: "OS: linux",
      exclude: false,
    });
  });

  it("parses platform arch exclude", () => {
    expect(humanizeConstraint("node.platform.arch != arm64")).toEqual({
      label: "Exclude arch arm64",
      exclude: true,
    });
  });

  it("parses node labels", () => {
    expect(humanizeConstraint("node.labels.region == us-east")).toEqual({
      label: "region = us-east",
      exclude: false,
    });
  });

  it("parses node labels exclude", () => {
    expect(humanizeConstraint("node.labels.env != staging")).toEqual({
      label: "env \u2260 staging",
      exclude: true,
    });
  });

  it("parses engine labels", () => {
    expect(humanizeConstraint("engine.labels.gpu == true")).toEqual({
      label: "engine gpu = true",
      exclude: false,
    });
  });

  it("handles extra whitespace around operator", () => {
    expect(humanizeConstraint("node.role   ==   manager")).toEqual({
      label: "Manager nodes only",
      exclude: false,
    });
  });
});
