import { test, expect } from "./fixtures";

test.describe("Metrics Console (/metrics)", () => {
  test("page loads with Query Console heading", async ({ page, monitoring }) => {
    test.skip(!monitoring?.prometheus, "Prometheus not available");

    await page.goto("/metrics");
    await expect(page.getByRole("heading", { name: "Query Console" })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("run query shows chart canvas or table", async ({ page, monitoring }) => {
    test.skip(!monitoring?.prometheus, "Prometheus not available");

    await page.goto("/metrics");
    await expect(page.getByRole("heading", { name: "Query Console" })).toBeVisible({
      timeout: 10_000,
    });

    // Type "up" in the query input and run it
    const input = page.getByRole("combobox");
    await input.fill("up");
    await input.press("ControlOrMeta+Enter");

    // Either a chart canvas or a table should appear
    await expect(page.locator("canvas, table").first()).toBeVisible({ timeout: 15_000 });
  });

  test("query persisted in URL: ?q=up auto-runs on load", async ({ page, monitoring }) => {
    test.skip(!monitoring?.prometheus, "Prometheus not available");

    await page.goto("/metrics?q=up");

    // Results should load automatically
    await expect(page.locator("canvas, table").first()).toBeVisible({ timeout: 15_000 });
  });

  test("range selector 6H updates URL to ?range=6h", async ({ page, monitoring }) => {
    test.skip(!monitoring?.prometheus, "Prometheus not available");

    // Start with a query already in the URL so range controls are visible
    await page.goto("/metrics?q=up");

    // Wait for results to load so range controls appear
    await expect(page.locator("canvas, table").first()).toBeVisible({ timeout: 15_000 });

    // Click the 6H range button
    await page.getByRole("button", { name: "6H" }).click();

    await expect(page).toHaveURL(/[?&]range=6h/);
  });
});
