import { test, expect } from "./fixtures";

test.describe("Metrics Panel", () => {
  test("range picker buttons exist on node detail page", async ({ page, monitoring }) => {
    test.skip(!monitoring?.prometheus, "Prometheus not available");

    await page.goto("/nodes");
    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });
    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/nodes\/.+/);

    // Wait for the MetricsPanel collapsible section to appear (title is "Metrics")
    const section = page.getByRole("button", { name: /^Metrics$/i });
    await expect(section).toBeVisible({ timeout: 15_000 });

    for (const label of ["1H", "6H", "24H", "7D"]) {
      await expect(page.getByRole("button", { name: label })).toBeVisible();
    }
  });

  test("refresh button exists on node detail page", async ({ page, monitoring }) => {
    test.skip(!monitoring?.prometheus, "Prometheus not available");

    await page.goto("/nodes");
    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });
    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/nodes\/.+/);

    // Wait for the MetricsPanel collapsible section to appear (title is "Metrics")
    const section = page.getByRole("button", { name: /^Metrics$/i });
    await expect(section).toBeVisible({ timeout: 15_000 });

    // Refresh button has a title of "Refresh"
    await expect(page.getByTitle("Refresh")).toBeVisible();
  });
});
