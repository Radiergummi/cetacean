import { defaultSecretTarget } from "./SecretsEditor";
import { describe, expect, it } from "vitest";

const current = { secretID: "s1", secretName: "app_secret", fileName: "/run/secrets/app_secret" };

describe("defaultSecretTarget", () => {
  it("reuses the path a removed secret vacated", () => {
    expect(defaultSecretTarget("app_secret_v2", [current], [])).toBe("/run/secrets/app_secret");
  });

  it("falls back to the secret's own name when no path was vacated", () => {
    expect(defaultSecretTarget("stack_db_password", [current], [current])).toBe(
      "/run/secrets/db_password",
    );
  });
});
