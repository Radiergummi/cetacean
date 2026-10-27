import { defaultTarget } from "./DataRefsEditor";
import { describe, expect, it } from "vitest";

const secret = { id: "s1", name: "app_secret", fileName: "/run/secrets/app_secret" };
const config = { id: "c1", name: "app_config", fileName: "/etc/app/config.yml" };

describe("defaultTarget", () => {
  it("reuses the path a removed secret vacated", () => {
    expect(defaultTarget("secret", "app_secret_v2", [secret], [])).toBe("/run/secrets/app_secret");
  });

  it("reuses the path a removed config vacated", () => {
    expect(defaultTarget("config", "app_config_v2", [config], [])).toBe("/etc/app/config.yml");
  });

  it("falls back to the secret's own name when no path was vacated", () => {
    expect(defaultTarget("secret", "stack_db_password", [secret], [secret])).toBe(
      "/run/secrets/db_password",
    );
  });

  it("falls back to the config's own name when no path was vacated", () => {
    expect(defaultTarget("config", "stack_nginx_conf", [config], [config])).toBe("/nginx_conf");
  });
});
