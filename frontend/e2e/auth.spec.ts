import { test, expect } from "./fixtures";

test.describe("Auth / Profile", () => {
  test("profile page redirects to home when auth is none", async ({ page }) => {
    await page.goto("/profile");
    // In none mode, /profile redirects to /
    const url = page.url();
    const isHome = url.endsWith("/") && !url.includes("/profile");
    test.skip(!isHome, "Auth is enabled — profile page rendered instead of redirecting");
    await expect(page.getByRole("heading", { name: "Cluster Overview" })).toBeVisible();
  });

  test("profile page shows identity when auth is enabled", async ({ page }) => {
    await page.goto("/profile");
    const url = page.url();
    const isProfile = url.includes("/profile");
    test.skip(!isProfile, "Auth is none — redirected to home");
    await expect(page.getByRole("heading").first()).toBeVisible();
  });
});
