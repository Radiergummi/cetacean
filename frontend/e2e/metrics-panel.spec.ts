import { test, expect, navigateToFirst } from "./fixtures";

test.describe("Metrics Panel", () => {
  test.beforeEach(async ({ page, monitoring }) => {
    test.skip(!monitoring?.prometheus, "Prometheus not available");

    await navigateToFirst(page, "/nodes", /\/nodes\/.+/);

    const section = page.getByRole("button", { name: /^Metrics$/i });
    await expect(section).toBeVisible({ timeout: 15_000 });
  });

  test("range picker buttons exist on node detail page", async ({ page }) => {
    /* eslint-disable no-await-in-loop */
    for (const label of ["1H", "6H", "24H", "7D"]) {
      await expect(page.getByRole("button", { name: label })).toBeVisible();
    }
    /* eslint-enable no-await-in-loop */
  });

  test("refresh button exists on node detail page", async ({ page }) => {
    await expect(page.getByTitle("Refresh")).toBeVisible();
  });
});
