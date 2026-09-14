import { test, expect } from "./fixtures";

test.describe("Error Pages", () => {
  test("404 page shows not found text and link back to home", async ({ page }) => {
    await page.goto("/this-does-not-exist");

    await expect(page.getByText(/404|not found/i).first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole("link", { name: /dashboard|home/i })).toBeVisible();
  });

  test("non-existent node resource shows error or not found", async ({ page }) => {
    await page.goto("/nodes/nonexistent-id-12345");

    // Either a FetchError component or the NotFound page renders.
    // FetchError renders "Failed to load node"; NotFound renders "Page not found" / "404".
    await expect(page.getByText(/404|not found|failed to load/i).first()).toBeVisible({
      timeout: 10_000,
    });
  });

  test("error index page heading is visible", async ({ page }) => {
    await page.goto("/api/errors");

    await expect(page.getByRole("heading", { name: /Error Reference/i })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("error code detail page renders from index link", async ({ page }) => {
    await page.goto("/api/errors");

    await expect(page.getByRole("heading", { name: /Error Reference/i })).toBeVisible({
      timeout: 10_000,
    });

    // The catalog is compiled in, so the table is never empty — just not
    // rendered yet when the heading appears.
    const codeLink = page.locator("table tbody tr a").first();
    await expect(codeLink).toBeVisible({ timeout: 10_000 });

    await codeLink.click();
    await expect(page).toHaveURL(/\/api\/errors\/.+/);
    await expect(page.getByRole("heading").first()).toBeVisible({ timeout: 10_000 });
  });
});
