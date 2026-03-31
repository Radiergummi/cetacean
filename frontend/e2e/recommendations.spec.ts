import { test, expect } from "./fixtures";

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

    for (const label of ["All", "Sizing", "Config", "Operational", "Cluster"]) {
      await expect(page.getByRole("button", { name: label, exact: true })).toBeVisible();
    }
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

  test("target links have href attributes", async ({ page }) => {
    await page.goto("/recommendations");
    await expect(page.getByRole("heading", { name: "Recommendations" })).toBeVisible({
      timeout: 10_000,
    });

    // Recommendation cards contain a severity icon with aria-label
    const cards = page.locator("[aria-label=info], [aria-label=warning], [aria-label=critical]");
    const cardCount = await cards.count();
    test.skip(cardCount === 0, "No recommendations present — target link test skipped");

    // Each card is inside a Collapsible.Root; find all anchor links in those ancestor elements
    const cardRoots = cards.first().locator("xpath=ancestor::div[contains(@class, 'rounded-lg')]");
    const links = cardRoots.locator("a");
    const linkCount = await links.count();

    for (let index = 0; index < linkCount; index++) {
      const href = await links.nth(index).getAttribute("href");
      expect(href).toBeTruthy();
    }
  });
});
