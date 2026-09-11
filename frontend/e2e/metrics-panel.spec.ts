import { test, expect, navigateToFirst } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * The header row of one named MetricsPanel: its disclosure toggle and, beside
 * it, that panel's own controls.
 *
 * Scoping matters because a node detail page renders two panels once cAdvisor
 * is reporting — "Metrics" and "Resource Usage by Stack" — and each brings its
 * own range picker and refresh button. Unscoped, every locator below matches
 * twice. This suite never caught that, because without a Prometheus to talk to
 * the whole file skipped: the specs could only pass in the environment that
 * declined to run them.
 */
function panelHeader(page: Page, name: RegExp) {
  return page.getByRole("button", { name }).locator("xpath=..");
}

test.describe("Metrics Panel", () => {
  test.beforeEach(async ({ page, monitoring }) => {
    test.skip(!monitoring?.prometheus, "Prometheus not available");

    await navigateToFirst(page, "/nodes", /\/nodes\/.+/);

    await expect(page.getByRole("button", { name: /^Metrics$/i })).toBeVisible({ timeout: 15_000 });
  });

  test("range picker buttons exist on node detail page", async ({ page }) => {
    const header = panelHeader(page, /^Metrics$/i);

    /* eslint-disable no-await-in-loop */
    for (const label of ["1H", "6H", "24H", "7D"]) {
      await expect(header.getByRole("button", { name: label })).toBeVisible();
    }
    /* eslint-enable no-await-in-loop */
  });

  test("refresh button exists on node detail page", async ({ page }) => {
    await expect(panelHeader(page, /^Metrics$/i).getByTitle("Refresh")).toBeVisible();
  });

  test("selecting a range puts it in the URL and keeps the charts mounted", async ({ page }) => {
    const header = panelHeader(page, /^Metrics$/i);

    await header.getByRole("button", { name: "6H" }).click();

    await expect(page).toHaveURL(/range=6h/);
    await expect(header.getByRole("button", { name: "6H" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    // The panel is still open and still showing charts, rather than having
    // thrown into its ErrorBoundary on the new range.
    await expect(page.locator("canvas").first()).toBeVisible({ timeout: 15_000 });
  });
});
