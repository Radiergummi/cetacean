import { diffLabels, rawLabelsForIntegration } from "./integrationLabels";
import { describe, expect, it } from "vitest";

describe("rawLabelsForIntegration", () => {
  it("returns empty array for null labels", () => {
    expect(rawLabelsForIntegration(null, "traefik")).toEqual([]);
  });

  it("returns empty array for unknown integration name", () => {
    expect(rawLabelsForIntegration({ "foo.bar": "baz" }, "unknown")).toEqual([]);
  });

  it("filters traefik labels from mixed labels", () => {
    const labels = {
      "traefik.enable": "true",
      "traefik.http.routers.app.rule": "Host(`app.example.com`)",
      "shepherd.enable": "true",
      "com.docker.stack.namespace": "mystack",
    };

    const result = rawLabelsForIntegration(labels, "traefik");

    expect(result).toEqual([
      ["traefik.enable", "true"],
      ["traefik.http.routers.app.rule", "Host(`app.example.com`)"],
    ]);
  });

  it("filters shepherd labels correctly", () => {
    const labels = {
      "shepherd.enable": "true",
      "shepherd.image-update": "monitor",
      "traefik.enable": "true",
    };

    const result = rawLabelsForIntegration(labels, "shepherd");

    expect(result).toEqual([
      ["shepherd.enable", "true"],
      ["shepherd.image-update", "monitor"],
    ]);
  });

  it("filters swarm-cronjob labels with swarm.cronjob. prefix", () => {
    const labels = {
      "swarm.cronjob.enable": "true",
      "swarm.cronjob.schedule": "0 * * * *",
      "swarm.other": "nope",
    };

    const result = rawLabelsForIntegration(labels, "swarm-cronjob");

    expect(result).toEqual([
      ["swarm.cronjob.enable", "true"],
      ["swarm.cronjob.schedule", "0 * * * *"],
    ]);
  });

  it("filters diun labels correctly", () => {
    const labels = {
      "diun.enable": "true",
      "diun.watch_repo": "true",
      "other.label": "value",
    };

    const result = rawLabelsForIntegration(labels, "diun");

    expect(result).toEqual([
      ["diun.enable", "true"],
      ["diun.watch_repo", "true"],
    ]);
  });

  it("returns empty array for empty labels object", () => {
    expect(rawLabelsForIntegration({}, "traefik")).toEqual([]);
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
