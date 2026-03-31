import { test, expect } from "./fixtures";

test.describe("Plugins (/plugins)", () => {
  test("renders Plugins heading", async ({ page }) => {
    await page.goto("/plugins");
    await expect(page.getByRole("heading", { name: "Plugins" })).toBeVisible({ timeout: 10_000 });
  });
});
