import { test, expect } from "./fixtures";

test.describe("Secret List (/secrets)", () => {
  test("renders heading", async ({ page }) => {
    await page.goto("/secrets");

    await expect(page.getByRole("heading", { name: "Secrets" })).toBeVisible({ timeout: 10_000 });
  });

  test("row click navigates to secret detail", async ({ page }) => {
    await page.goto("/secrets");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/secrets\/.+/);
  });
});

test.describe("Secret Detail (/secrets/:id)", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/secrets");
    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });
    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/secrets\/.+/);
  });

  test("shows ID, Created, and Updated metadata", async ({ page }) => {
    await expect(page.getByText("ID", { exact: true })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Created", { exact: true })).toBeVisible();
    await expect(page.getByText("Updated", { exact: true })).toBeVisible();
  });

  test("labels section renders", async ({ page }) => {
    await expect(page.getByRole("button", { name: /^Labels$/i })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("used by services section renders", async ({ page }) => {
    await expect(page.getByRole("button", { name: /Used by Services/i })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("activity section renders when history exists", async ({ page }) => {
    await expect(page.getByText("ID", { exact: true })).toBeVisible({ timeout: 10_000 });

    const activityButton = page.getByRole("button", { name: /Recent Activity/i });
    const count = await activityButton.count();

    if (count > 0) {
      await expect(activityButton).toBeVisible();
    }
  });

  test("remove button is present", async ({ page }) => {
    await expect(page.getByRole("button", { name: /Remove/i })).toBeVisible({ timeout: 10_000 });
  });
});
