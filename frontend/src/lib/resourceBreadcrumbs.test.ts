import { resourceBreadcrumbs, stripStackPrefix } from "./resourceBreadcrumbs";
import { describe, expect, it } from "vitest";

describe("resourceBreadcrumbs", () => {
  it("routes a stacked resource through its stack", () => {
    expect(
      resourceBreadcrumbs({
        listLabel: "Services",
        listPath: "/services",
        name: "monitoring_prometheus",
        stack: "monitoring",
      }),
    ).toEqual([
      { label: "Stacks", to: "/stacks" },
      { label: "monitoring", to: "/stacks/monitoring" },
      { label: "prometheus" },
    ]);
  });

  it("routes an unstacked resource through its own list", () => {
    expect(
      resourceBreadcrumbs({
        listLabel: "Volumes",
        listPath: "/volumes",
        name: "scratch",
      }),
    ).toEqual([{ label: "Volumes", to: "/volumes" }, { label: "scratch" }]);
  });

  it("links the resource when further crumbs follow it", () => {
    expect(
      resourceBreadcrumbs({
        listLabel: "Services",
        listPath: "/services",
        name: "monitoring_prometheus",
        stack: "monitoring",
        to: "/services/abc123",
        trail: [{ label: "Replica #2" }],
      })[2],
    ).toEqual({ label: "prometheus", to: "/services/abc123" });
  });

  it("leaves the leaf unlinked when it is the last crumb", () => {
    const crumbs = resourceBreadcrumbs({
      listLabel: "Services",
      listPath: "/services",
      name: "web",
      to: "/services/abc123",
    });

    expect(crumbs[crumbs.length - 1]).toEqual({ label: "web" });
  });

  it("escapes a stack name for the URL", () => {
    expect(
      resourceBreadcrumbs({
        listLabel: "Services",
        listPath: "/services",
        name: "a b_web",
        stack: "a b",
      })[1],
    ).toEqual({ label: "a b", to: "/stacks/a%20b" });
  });
});

describe("stripStackPrefix", () => {
  it("strips the stack namespace prefix", () => {
    expect(stripStackPrefix("monitoring_prometheus", "monitoring")).toBe("prometheus");
  });

  it("keeps a name that only joined the stack by label", () => {
    expect(stripStackPrefix("prometheus", "monitoring")).toBe("prometheus");
  });

  it("never guesses at a prefix when there is no stack", () => {
    // An underscore in an unstacked resource's name is just an underscore.
    expect(stripStackPrefix("my_volume")).toBe("my_volume");
  });
});
