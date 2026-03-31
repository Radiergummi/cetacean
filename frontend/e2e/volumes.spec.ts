import { test, expect } from "./fixtures";

test.describe("Volume List (/volumes)", () => {
  test("renders heading", async ({ page }) => {
    await page.goto("/volumes");

    await expect(page.getByRole("heading", { name: "Volumes" })).toBeVisible({ timeout: 10_000 });
  });

  test("row click navigates to volume detail", async ({ page }) => {
    await page.goto("/volumes");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/volumes\/.+/);
  });
});

test.describe("Volume Detail (/volumes/:name)", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/volumes");
    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });
    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/volumes\/.+/);
  });

  test("shows Driver, Scope, and Mountpoint metadata", async ({ page }) => {
    await expect(page.getByText("Driver")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Scope")).toBeVisible();
    await expect(page.getByText("Mountpoint")).toBeVisible();
  });

  test("Remove button is present", async ({ page }) => {
    await expect(page.getByRole("button", { name: /Remove/i })).toBeVisible({ timeout: 10_000 });
  });
});
