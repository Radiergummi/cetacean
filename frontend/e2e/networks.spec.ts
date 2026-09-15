import { test, expect, navigateToFirst, clickRow } from "./fixtures";

test.describe("Network List (/networks)", () => {
  test("renders heading", async ({ page }) => {
    await page.goto("/networks");

    await expect(page.getByRole("heading", { name: "Networks" })).toBeVisible({ timeout: 10_000 });
  });

  test("row click navigates to network detail", async ({ page }) => {
    await page.goto("/networks");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await clickRow(page.locator("table tbody tr").first());
    await expect(page).toHaveURL(/\/networks\/.+/);
  });
});

test.describe("Network Detail (/networks/:id)", () => {
  test.beforeEach(async ({ page }) => {
    await navigateToFirst(page, "/networks", /\/networks\/.+/);
  });

  test("shows Driver and Scope metadata", async ({ page }) => {
    // Exact, because a network carrying driver options also renders a "Driver
    // Options" section and a `com.docker.network.driver.mtu` option key, and a
    // substring match on "Driver" resolves to all three.
    await expect(page.getByText("Driver", { exact: true })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Scope", { exact: true })).toBeVisible();
  });

  test("services section is present", async ({ page }) => {
    // ServiceRefList renders a CollapsibleSection with title "Connected Services"
    await expect(page.getByRole("button", { name: /Connected Services/i })).toBeVisible({
      timeout: 10_000,
    });
  });
});
