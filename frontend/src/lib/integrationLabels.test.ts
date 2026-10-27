import { diffLabels, diffModelledLabels, rawLabelsForIntegration } from "./integrationLabels";
import { describe, expect, it } from "vitest";

describe("rawLabelsForIntegration", () => {
  it("returns empty array for null labels", () => {
    expect(rawLabelsForIntegration(null, ["traefik.enable"])).toEqual([]);
  });

  it("keeps only the labels the integration claimed", () => {
    const labels = {
      "traefik.enable": "true",
      "traefik.http.routers.app.rule": "Host(`app.example.com`)",
      "shepherd.enable": "true",
      "com.docker.stack.namespace": "mystack",
    };

    const result = rawLabelsForIntegration(labels, [
      "traefik.enable",
      "traefik.http.routers.app.rule",
    ]);

    expect(result).toEqual([
      ["traefik.enable", "true"],
      ["traefik.http.routers.app.rule", "Host(`app.example.com`)"],
    ]);
  });

  it("skips a claimed label the service no longer has", () => {
    expect(rawLabelsForIntegration({ caddy: "example.com" }, ["caddy", "caddy.encode"])).toEqual([
      ["caddy", "example.com"],
    ]);
  });
});

describe("diffLabels", () => {
  it("produces an add op for a new key", () => {
    const original: [string, string][] = [];
    const result = diffLabels(original, { "new.key": "value" });

    expect(result).toEqual([{ op: "add", path: "/new.key", value: "value" }]);
  });

  it("produces a replace op for a changed value", () => {
    const original: [string, string][] = [["my.key", "old"]];
    const result = diffLabels(original, { "my.key": "new" });

    expect(result).toEqual([{ op: "replace", path: "/my.key", value: "new" }]);
  });

  it("produces a remove op for a deleted key", () => {
    const original: [string, string][] = [["gone.key", "value"]];
    const result = diffLabels(original, {});

    expect(result).toEqual([{ op: "remove", path: "/gone.key" }]);
  });

  it("produces empty array when nothing changed", () => {
    const original: [string, string][] = [
      ["a", "1"],
      ["b", "2"],
    ];
    const result = diffLabels(original, { a: "1", b: "2" });

    expect(result).toEqual([]);
  });
});

describe("diffModelledLabels", () => {
  const raw: [string, string][] = [
    ["traefik.enable", "true"],
    ["traefik.http.routers.web.rule", "Host(`a.example`)"],
    ["traefik.http.routers.web.entrypoints", "web, websecure"],
    ["traefik.http.routers.web.tls", "true"],
    ["traefik.http.routers.web.tls.options", "modern@file"],
    ["traefik.tcp.routers.db.rule", "HostSNI(`*`)"],
    ["traefik.tcp.services.db.loadbalancer.server.port", "5432"],
  ];
  const before = {
    "traefik.enable": "true",
    "traefik.http.routers.web.rule": "Host(`a.example`)",
    "traefik.http.routers.web.entrypoints": "web,websecure",
  };

  it("produces no operations for a save without changes", () => {
    expect(diffModelledLabels(raw, before, { ...before })).toEqual([]);
  });

  it("never touches labels the editor does not model", () => {
    const ops = diffModelledLabels(raw, before, { "traefik.enable": "false" });

    expect(ops).toEqual([
      { op: "replace", path: "/traefik.enable", value: "false" },
      { op: "remove", path: "/traefik.http.routers.web.rule" },
      { op: "remove", path: "/traefik.http.routers.web.entrypoints" },
    ]);
  });

  it("adds a key the service did not have", () => {
    const after = { ...before, "traefik.http.routers.web.priority": "10" };

    expect(diffModelledLabels(raw, before, after)).toEqual([
      { op: "add", path: "/traefik.http.routers.web.priority", value: "10" },
    ]);
  });
});
