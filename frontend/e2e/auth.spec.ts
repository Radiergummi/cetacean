import { test, expect } from "./fixtures";

test.describe("Auth / Profile", () => {
  test("profile page shows profile content or redirects to home", async ({ page }) => {
    await page.goto("/profile");

    // Either the profile page renders (authenticated) or it redirects to "/"
    const isHome = page.url().endsWith("/") || page.url().match(/\/$/) !== null;

    if (isHome) {
      // Redirected to dashboard — auth mode is "none", this is expected
      await expect(page.getByRole("heading", { name: "Cluster Overview" })).toBeVisible({
        timeout: 10_000,
      });
    } else {
      // Profile page rendered — should show a heading with identity info
      await expect(page.getByRole("heading").first()).toBeVisible({ timeout: 10_000 });
    }
  });
});
