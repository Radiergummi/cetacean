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

    // Either recommendation cards or an empty state message should be present
    const cards = page.locator(".rounded-lg.border.bg-card");
    const emptyState = page.getByText(/No recommendations/i);
    await expect(cards.first().or(emptyState)).toBeVisible({ timeout: 10_000 });
  });

  test("target links have href attributes", async ({ page }) => {
    await page.goto("/recommendations");
    await expect(page.getByRole("heading", { name: "Recommendations" })).toBeVisible({
      timeout: 10_000,
    });

    // Check if there are any recommendation cards at all
    const cards = page.locator(".rounded-lg.border.bg-card");
    const cardCount = await cards.count();

    if (cardCount === 0) {
      test.skip(true, "No recommendations present — target link test skipped");
    }

    // All anchor links inside cards should have a non-empty href
    const links = cards.locator("a");
    const linkCount = await links.count();

    for (let index = 0; index < linkCount; index++) {
      const href = await links.nth(index).getAttribute("href");
      expect(href).toBeTruthy();
    }
  });
});
