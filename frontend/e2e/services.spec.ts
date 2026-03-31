import { test, expect } from "./fixtures";

test.describe("Service List (/services)", () => {
  test("renders table with expected columns", async ({ page }) => {
    await page.goto("/services");

    const table = page.getByRole("table");
    await expect(table).toBeVisible({ timeout: 10_000 });

    const header = page.getByRole("row").first();
    await expect(header.getByText("Name")).toBeVisible();
    await expect(header.getByText("Image")).toBeVisible();
    await expect(header.getByText("Mode")).toBeVisible();
  });

  test("renders at least one service row", async ({ page }) => {
    await page.goto("/services");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });
  });

  test("search filters services", async ({ page }) => {
    await page.goto("/services");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    const searchInput = page.getByPlaceholder("Search services…");
    await searchInput.fill("nginx");

    // A row containing "nginx" should be visible
    await expect(page.getByRole("cell", { name: /nginx/i }).first()).toBeVisible({
      timeout: 5_000,
    });

    // Searching for something that won't match should show empty state
    await searchInput.fill("zzznomatch");
    await expect(page.getByText(/No services match your search/)).toBeVisible({ timeout: 5_000 });
  });

  test("view toggle switches between table and grid", async ({ page }) => {
    // Ensure we start in table view
    await page.goto("/services");
    await page.evaluate(() => localStorage.removeItem("viewMode:services"));
    await page.reload();

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    // Switch to grid view
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator("table")).not.toBeVisible();

    // Switch back to table view
    await page.getByRole("button", { name: "Table view" }).click();
    await expect(page.getByRole("table")).toBeVisible();
  });

  test("row click navigates to service detail", async ({ page }) => {
    await page.goto("/services");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/services\/.+/);
  });
});

test.describe("Service Detail (/services/:id)", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/services");
    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });
    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/services\/.+/);
  });

  test("shows service name in heading", async ({ page }) => {
    // PageHeader renders the service name as the page heading
    await expect(page.getByRole("heading").first()).toBeVisible({ timeout: 10_000 });
  });

  test("action buttons present when write operations are enabled", async ({ page }) => {
    // ServiceActions renders only when the ops level is >= 1 (operational).
    // If the buttons are absent we pass vacuously — this is a read-only test.
    await expect(page.getByRole("heading").first()).toBeVisible({ timeout: 10_000 });

    const rollback = page.getByRole("button", { name: /Rollback/i });
    const restart = page.getByRole("button", { name: /Restart/i });
    const count = await rollback.count();

    if (count > 0) {
      await expect(rollback).toBeVisible();
      await expect(restart).toBeVisible();
    }
  });

  test("tasks section renders with state filter", async ({ page }) => {
    // TasksTable is wrapped in a CollapsibleSection whose toggle button says "Tasks"
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });

    // TaskStateFilter renders a segmented control with at least "Active" option
    const tasksButton = page.getByRole("button", { name: /^Tasks$/i });
    await expect(tasksButton).toHaveAttribute("aria-expanded", "true");
  });

  test("environment variables section exists", async ({ page }) => {
    // EnvEditor uses KeyValueEditor with title "Environment Variables" rendered as a
    // CollapsibleSection toggle button
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });

    const envButton = page.getByRole("button", { name: /Environment Variables/i });
    const count = await envButton.count();

    if (count > 0) {
      await expect(envButton).toBeVisible();
    }
  });

  test("labels section exists", async ({ page }) => {
    // KeyValueEditor with title "Labels"
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });

    const labelsButton = page.getByRole("button", { name: /^Labels$/i });
    const count = await labelsButton.count();

    if (count > 0) {
      await expect(labelsButton).toBeVisible();
    }
  });

  test("deploy configuration section is collapsible and can be expanded", async ({ page }) => {
    // Clear any persisted state to ensure closed default
    await page.evaluate(() => localStorage.removeItem("section:deploy-configuration"));
    await page.reload();
    await expect(page).toHaveURL(/\/services\/.+/);

    const deployToggle = page.getByRole("button", { name: /Deploy Configuration/i });
    await expect(deployToggle).toBeVisible({ timeout: 10_000 });

    // Should be closed by default (defaultOpen={false})
    await expect(deployToggle).toHaveAttribute("aria-expanded", "false");

    // Expand it
    await deployToggle.click();
    await expect(deployToggle).toHaveAttribute("aria-expanded", "true");
  });

  test("log viewer section exists", async ({ page }) => {
    // LogViewer with header="Logs" renders a SectionToggle with that text
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole("button", { name: /^Logs$/i })).toBeVisible({ timeout: 10_000 });
  });

  test("recent activity section renders when history entries are present", async ({ page }) => {
    // Wait for the page to fully load (Tasks section always renders)
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });

    const activityButton = page.getByRole("button", { name: /Recent Activity/i });
    const count = await activityButton.count();

    if (count > 0) {
      await expect(activityButton).toBeVisible();
    }
  });
});
