import { test, expect, navigateToFirst } from "./fixtures";

test.describe("Task List (/tasks)", () => {
  test("renders table with expected columns", async ({ page }) => {
    await page.goto("/tasks");

    const table = page.getByRole("table");
    await expect(table).toBeVisible({ timeout: 10_000 });

    const header = page.getByRole("row").first();
    await expect(header.getByText("Service")).toBeVisible();
    await expect(header.getByText("State")).toBeVisible();
    await expect(header.getByText("Node")).toBeVisible();
  });

  test("row click navigates to task detail", async ({ page }) => {
    await page.goto("/tasks");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/tasks\/.+/);
  });
});

test.describe("Task Detail (/tasks/:id)", () => {
  test.beforeEach(async ({ page }) => {
    await navigateToFirst(page, "/tasks", /\/tasks\/.+/);
  });

  test("shows state, service, and image metadata", async ({ page }) => {
    await expect(page.getByText("State", { exact: true })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Service", { exact: true })).toBeVisible();
    await expect(page.getByText("Image", { exact: true })).toBeVisible();
  });

  test("log viewer renders", async ({ page }) => {
    await expect(page.getByRole("button", { name: "Logs", exact: true })).toBeVisible({
      timeout: 10_000,
    });
  });
});
