import { canAccess } from "./useCanAccess";
import { describe, it, expect } from "vitest";

describe("canAccess", () => {
  it("allows everything when no policy is active", () => {
    expect(canAccess(undefined, "service:api", "write")).toBe(true);
  });

  it("matches a grant on the resource itself", () => {
    const permissions = { "service:api": ["read"] };

    expect(canAccess(permissions, "service:api", "read")).toBe(true);
    expect(canAccess(permissions, "service:other", "read")).toBe(false);
  });

  it("treats write as implying read, but not the reverse", () => {
    expect(canAccess({ "service:api": ["write"] }, "service:api", "read")).toBe(true);
    expect(canAccess({ "service:api": ["read"] }, "service:api", "write")).toBe(false);
  });

  it("matches the type exactly, so a name glob cannot cross types", () => {
    const permissions = { "service:*": ["read"] };

    expect(canAccess(permissions, "service:api", "read")).toBe(true);
    expect(canAccess(permissions, "node:api", "read")).toBe(false);
  });

  it("supports both glob wildcards the server accepts", () => {
    expect(canAccess({ "service:web-*": ["read"] }, "service:web-api", "read")).toBe(true);
    expect(canAccess({ "service:web?": ["read"] }, "service:web1", "read")).toBe(true);
    expect(canAccess({ "service:web?": ["read"] }, "service:web", "read")).toBe(false);
  });

  it("matches everything on a bare wildcard", () => {
    expect(canAccess({ "*": ["write"] }, "volume:data", "write")).toBe(true);
  });

  it("never matches an empty resource name", () => {
    expect(canAccess({ "service:*": ["read"] }, "service:", "read")).toBe(false);
  });

  // The gap this hook had: a grant reaches a resource through the stack it
  // belongs to, exactly as the server's grantMatchesResource does. Comparing
  // patterns to the resource alone hid actions the server would have allowed.
  it("reaches a resource through the stack it belongs to", () => {
    const permissions = { "stack:web": ["write"] };

    expect(canAccess(permissions, "service:web_api", "write", { stack: "web" })).toBe(true);
    expect(canAccess(permissions, "service:web_api", "write")).toBe(false);
    expect(canAccess(permissions, "node:worker-1", "write", { stack: "web" })).toBe(false);
  });

  it("reaches a task through its parent service", () => {
    const permissions = { "service:web_api": ["write"] };

    expect(canAccess(permissions, "task:abc123", "write", { service: "web_api" })).toBe(true);
    expect(canAccess(permissions, "task:abc123", "write")).toBe(false);
  });

  it("reaches a task through its parent service's stack", () => {
    const permissions = { "stack:web": ["read"] };

    expect(
      canAccess(permissions, "task:abc123", "read", { service: "web_api", stack: "web" }),
    ).toBe(true);
  });
});
