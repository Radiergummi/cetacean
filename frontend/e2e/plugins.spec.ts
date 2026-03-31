import { test, expect } from "./fixtures";

test.describe("Plugins (/plugins)", () => {
  test("renders Plugins heading", async ({ page }) => {
    await page.goto("/plugins");
    await expect(page.getByRole("heading", { name: "Plugins" })).toBeVisible({ timeout: 10_000 });
  });

  test("plugin name link navigates to plugin detail", async ({ page }) => {
    await page.goto("/plugins");
    await expect(page.getByRole("heading", { name: "Plugins" })).toBeVisible({ timeout: 10_000 });

    const pluginLinks = page.locator("table tbody tr a");
    const linkCount = await pluginLinks.count();
    test.skip(linkCount === 0, "No plugins installed — cannot test detail navigation");

    await pluginLinks.first().click();
    await expect(page).toHaveURL(/\/plugins\/.+/);
    await expect(page.getByRole("heading").first()).toBeVisible({ timeout: 10_000 });
  });
});
