import { imageRegistryUrl } from "./imageUrl";
import { describe, it, expect } from "vitest";

describe("imageRegistryUrl", () => {
  it("links official Docker Hub images", () => {
    expect(imageRegistryUrl("nginx")).toBe("https://hub.docker.com/_/nginx");
    expect(imageRegistryUrl("nginx:1.25")).toBe("https://hub.docker.com/_/nginx");
    expect(imageRegistryUrl("nginx@sha256:abc")).toBe("https://hub.docker.com/_/nginx");
    expect(imageRegistryUrl("docker.io/library/nginx")).toBe("https://hub.docker.com/_/nginx");
  });

  it("links Docker Hub user images", () => {
    expect(imageRegistryUrl("myuser/myimage")).toBe("https://hub.docker.com/r/myuser/myimage");
    expect(imageRegistryUrl("docker.io/myuser/myimage:2")).toBe(
      "https://hub.docker.com/r/myuser/myimage",
    );
  });

  it("links the registries it knows", () => {
    expect(imageRegistryUrl("ghcr.io/owner/repo:latest")).toBe("https://ghcr.io/owner/repo");
    expect(imageRegistryUrl("quay.io/owner/repo")).toBe("https://quay.io/repository/owner/repo");
    expect(imageRegistryUrl("gcr.io/project/image")).toBe("https://gcr.io/project/image");
    expect(imageRegistryUrl("eu.gcr.io/project/image")).toBe("https://eu.gcr.io/project/image");
  });

  it("returns null for a registry it does not know", () => {
    expect(imageRegistryUrl("registry.example.com/team/image")).toBeNull();
  });

  it("returns null for a private registry published on a port", () => {
    expect(imageRegistryUrl("registry.example.com:5000/team/image")).toBeNull();
    expect(imageRegistryUrl("registry.example.com:5000/team/image:1.0")).toBeNull();
    expect(imageRegistryUrl("localhost:5000/image")).toBeNull();
  });
});
