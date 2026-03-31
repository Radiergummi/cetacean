import { test, expect } from "./fixtures";

test.describe("Stack List (/stacks)", () => {
  test("renders heading and table", async ({ page }) => {
    await page.goto("/stacks");

    await expect(page.getByRole("heading", { name: "Stacks" })).toBeVisible({ timeout: 10_000 });

    const table = page.getByRole("table");
    await expect(table).toBeVisible({ timeout: 10_000 });
  });

  test("row click navigates to stack detail", async ({ page }) => {
    await page.goto("/stacks");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/stacks\/.+/);
  });
});

test.describe("Stack Detail (/stacks/cetacean-monitoring)", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/stacks/cetacean-monitoring");
    // Wait for the page to finish loading (heading is rendered after fetch)
    await expect(page.getByRole("heading", { name: "cetacean-monitoring" })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("shows stack name in heading", async ({ page }) => {
    await expect(page.getByRole("heading", { name: "cetacean-monitoring" })).toBeVisible();
  });

  test("Services section is present", async ({ page }) => {
    await expect(page.getByRole("button", { name: /^Services$/i })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("Services section is collapsible", async ({ page }) => {
    const toggle = page.getByRole("button", { name: /^Services$/i });
    await expect(toggle).toBeVisible({ timeout: 10_000 });

    // Section should be open by default — the services table should be visible
    await expect(toggle).toHaveAttribute("aria-expanded", "true");

    // Collapse
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "false");

    // Expand again
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
  });
});
