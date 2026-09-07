import { isReservedLabelKey, validateLabelKey } from "./labelValidation";
import { describe, it, expect } from "vitest";

describe("isReservedLabelKey", () => {
  it("recognises the three reserved Docker namespaces", () => {
    expect(isReservedLabelKey("com.docker.stack.namespace")).toBe(true);
    expect(isReservedLabelKey("io.docker.something")).toBe(true);
    expect(isReservedLabelKey("org.dockerproject.thing")).toBe(true);
  });

  it("leaves everything else alone", () => {
    expect(isReservedLabelKey("com.example.team")).toBe(false);
    expect(isReservedLabelKey("traefik.enable")).toBe(false);
  });
});

describe("validateLabelKey", () => {
  it("stays quiet on empty input so the field is not red while typing", () => {
    expect(validateLabelKey("")).toBeNull();
  });

  it("accepts the key shapes the docs describe", () => {
    expect(validateLabelKey("environment")).toBeNull();
    expect(validateLabelKey("com.example.team")).toBeNull();
    expect(validateLabelKey("com.example/group")).toBeNull();
    expect(validateLabelKey("node1")).toBeNull();
  });

  it("accepts hyphens, which the format explicitly allows", () => {
    expect(validateLabelKey("my-label")).toBeNull();
    expect(validateLabelKey("com.example.my-team/sub-group")).toBeNull();
  });

  it("rejects reserved namespaces", () => {
    expect(validateLabelKey("com.docker.stack.namespace")).toMatch(/reserved/);
  });

  it("rejects keys that do not start and end alphanumerically", () => {
    expect(validateLabelKey(".leading")).not.toBeNull();
    expect(validateLabelKey("trailing.")).not.toBeNull();
    expect(validateLabelKey("-leading")).not.toBeNull();
    expect(validateLabelKey("trailing-")).not.toBeNull();
  });

  it("rejects uppercase", () => {
    expect(validateLabelKey("Environment")).not.toBeNull();
  });

  it("rejects consecutive separators", () => {
    expect(validateLabelKey("a..b")).toMatch(/[Cc]onsecutive/);
    expect(validateLabelKey("a--b")).toMatch(/[Cc]onsecutive/);
  });
});
