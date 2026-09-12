import { test, expect, apiJson } from "./fixtures";

test.describe("Recommendations (/recommendations)", () => {
  test("page loads with Recommendations heading", async ({ page }) => {
    await page.goto("/recommendations");
    await expect(page.getByRole("heading", { name: "Recommendations" })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("filter tabs All, Sizing, Config, Operational, Cluster are visible", async ({ page }) => {
    await page.goto("/recommendations");
    await expect(page.getByRole("heading", { name: "Recommendations" })).toBeVisible({
      timeout: 10_000,
    });

    /* eslint-disable no-await-in-loop */
    for (const label of ["All", "Sizing", "Config", "Operational", "Cluster"]) {
      await expect(page.getByRole("button", { name: label, exact: true })).toBeVisible();
    }
    /* eslint-enable no-await-in-loop */
  });

  test("clicking Config tab updates URL to ?filter=config", async ({ page }) => {
    await page.goto("/recommendations");
    await expect(page.getByRole("heading", { name: "Recommendations" })).toBeVisible({
      timeout: 10_000,
    });

    await page.getByRole("button", { name: "Config", exact: true }).click();
    await expect(page).toHaveURL(/[?&]filter=config/);
  });

  test("empty state or recommendation cards are visible", async ({ page }) => {
    await page.goto("/recommendations");
    await expect(page.getByRole("heading", { name: "Recommendations" })).toBeVisible({
      timeout: 10_000,
    });

    // Recommendation cards contain a severity icon with aria-label; match any of the known values.
    // Fall back to the empty state message if no cards exist.
    const cards = page.locator("[aria-label=info], [aria-label=warning], [aria-label=critical]");
    const emptyState = page.getByText(/No recommendations/i);
    await expect(cards.first().or(emptyState)).toBeVisible({ timeout: 10_000 });
  });

  test("target links have href attributes", async ({ page, request, baseURL }) => {
    await page.goto("/recommendations");
    await expect(page.getByRole("heading", { name: "Recommendations" })).toBeVisible({
      timeout: 10_000,
    });

    // The heading renders before the fetch behind these cards resolves.
    const recommendations = await apiJson(request, baseURL, "/recommendations");
    const items = Array.isArray(recommendations.items) ? recommendations.items : [];
    test.skip(items.length === 0, "No recommendations present — target link test skipped");

    const cards = page.locator("[aria-label=info], [aria-label=warning], [aria-label=critical]");
    await expect(cards.first()).toBeVisible({ timeout: 10_000 });

    // Recommendation cards contain target links (service/node names)
    const links = page.locator("a[href^='/services/'], a[href^='/nodes/']");
    const linkCount = await links.count();
    expect(linkCount).toBeGreaterThan(0);
  });
});
