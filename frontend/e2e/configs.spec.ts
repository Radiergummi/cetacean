import { test, expect, apiJson, detailId, hasHistory, navigateToFirst, clickRow } from "./fixtures";

test.describe("Config List (/configs)", () => {
  test("renders heading", async ({ page }) => {
    await page.goto("/configs");

    await expect(page.getByRole("heading", { name: "Configs" })).toBeVisible({ timeout: 10_000 });
  });

  test("row click navigates to config detail", async ({ page }) => {
    await page.goto("/configs");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await clickRow(page.locator("table tbody tr").first());
    await expect(page).toHaveURL(/\/configs\/.+/);
  });
});

test.describe("Config Detail (/configs/:id)", () => {
  test.beforeEach(async ({ page }) => {
    await navigateToFirst(page, "/configs", /\/configs\/.+/);
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

  test("data section with Copy button renders when data is present", async ({
    page,
    request,
    baseURL,
  }) => {
    // Wait for page to finish loading
    await expect(page.getByText("ID", { exact: true })).toBeVisible({ timeout: 10_000 });

    const detail = await apiJson(request, baseURL, `/configs/${detailId(page)}`);
    const config = (detail.config ?? {}) as Record<string, unknown>;
    const spec = (config.Spec ?? {}) as Record<string, unknown>;
    test.skip(!spec.Data, "Config carries no data");

    await expect(page.getByRole("button", { name: /^Data$/i })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole("button", { name: /Copy/i })).toBeVisible({ timeout: 10_000 });
  });

  test("used by services section renders", async ({ page }) => {
    await expect(page.getByRole("button", { name: /Used by Services/i })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("activity section renders when history exists", async ({ page, request, baseURL }) => {
    // ActivitySection returns null for an empty feed, fetched after mount —
    // so absence right now means "not loaded yet" as readily as "no history".
    await expect(page.getByText("ID", { exact: true })).toBeVisible({ timeout: 10_000 });

    const present = await hasHistory(request, baseURL, detailId(page));
    test.skip(!present, "No activity history recorded for this config");

    await expect(page.getByRole("button", { name: /Recent Activity/i })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("remove button is present", async ({ page }) => {
    await expect(page.getByRole("button", { name: /Remove/i })).toBeVisible({ timeout: 10_000 });
  });
});
