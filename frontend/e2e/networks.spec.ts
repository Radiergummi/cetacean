import { test, expect, navigateToFirst } from "./fixtures";

test.describe("Network List (/networks)", () => {
  test("renders heading", async ({ page }) => {
    await page.goto("/networks");

    await expect(page.getByRole("heading", { name: "Networks" })).toBeVisible({ timeout: 10_000 });
  });

  test("row click navigates to network detail", async ({ page }) => {
    await page.goto("/networks");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/networks\/.+/);
  });
});

test.describe("Network Detail (/networks/:id)", () => {
  test.beforeEach(async ({ page }) => {
    await navigateToFirst(page, "/networks", /\/networks\/.+/);
  });

  test("shows Driver and Scope metadata", async ({ page }) => {
    await expect(page.getByText("Driver")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Scope")).toBeVisible();
  });

  test("services section is present", async ({ page }) => {
    // ServiceRefList renders a CollapsibleSection with title "Connected Services"
    await expect(page.getByRole("button", { name: /Connected Services/i })).toBeVisible({
      timeout: 10_000,
    });
  });
});
