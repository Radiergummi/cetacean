import { defaultConfigTarget } from "./ConfigsEditor";
import { describe, expect, it } from "vitest";

const current = { configID: "c1", configName: "app_config", fileName: "/etc/app/config.yml" };

describe("defaultConfigTarget", () => {
  it("reuses the path a removed config vacated", () => {
    expect(defaultConfigTarget("app_config_v2", [current], [])).toBe("/etc/app/config.yml");
  });

  it("falls back to the config's own name when no path was vacated", () => {
    expect(defaultConfigTarget("stack_nginx_conf", [current], [current])).toBe("/nginx_conf");
  });
});
