import { test, expect, navigateToFirst } from "./fixtures";

/**
 * Navigate to the first service detail page and expand the Logs section.
 * Returns the locator for the log viewer container.
 */
async function openLogViewer(page: Parameters<Parameters<typeof test>[1]>[0]) {
  await navigateToFirst(page, "/services", /\/services\/.+/);

  // Ensure the Logs toggle is visible and expand it if collapsed
  const logsToggle = page.getByRole("button", { name: /^Logs$/i });
  await expect(logsToggle).toBeVisible({ timeout: 10_000 });

  const expanded = await logsToggle.getAttribute("aria-expanded");

  if (expanded !== "true") {
    await logsToggle.click();
    await expect(logsToggle).toHaveAttribute("aria-expanded", "true");
  }

  // The log viewer renders into a div#logs once expanded
  return page.locator("#logs");
}

test.describe("Log Viewer (service detail)", () => {
  test("Logs section toggle button is present", async ({ page }) => {
    await navigateToFirst(page, "/services", /\/services\/.+/);

    await expect(page.getByRole("button", { name: /^Logs$/i })).toBeVisible({ timeout: 10_000 });
  });

  test("expanding the Logs section reveals the toolbar", async ({ page }) => {
    const logViewer = await openLogViewer(page);

    await expect(logViewer.getByRole("toolbar")).toBeVisible({ timeout: 10_000 });
  });

  test("time range selector button is visible", async ({ page }) => {
    const logViewer = await openLogViewer(page);

    // TimeRangeSelector renders a button with a Clock icon and a label like "Last 5m", "All", etc.
    const timeRangeButton = logViewer.getByRole("button", { name: /Last|All|time/i }).first();
    await expect(timeRangeButton).toBeVisible({ timeout: 10_000 });
  });

  test("stream filter toggle shows stdout/stderr/all options", async ({ page }) => {
    const logViewer = await openLogViewer(page);

    // StreamFilterToggle renders three buttons: All, stdout, stderr
    await expect(logViewer.getByRole("button", { name: "stdout", exact: true })).toBeVisible({
      timeout: 10_000,
    });
    await expect(logViewer.getByRole("button", { name: "stderr", exact: true })).toBeVisible();
  });

  test("search input is present within the log viewer", async ({ page }) => {
    const logViewer = await openLogViewer(page);

    const searchInput = logViewer.getByPlaceholder("Filter logs…");
    await expect(searchInput).toBeVisible({ timeout: 10_000 });
  });

  test("stream filter toggle is interactive", async ({ page }) => {
    const logViewer = await openLogViewer(page);

    const stdoutButton = logViewer.getByRole("button", { name: "stdout", exact: true });
    await expect(stdoutButton).toBeVisible({ timeout: 10_000 });

    // Click stdout — it should become pressed
    await stdoutButton.click();
    await expect(stdoutButton).toHaveAttribute("aria-pressed", "true");

    // Click All to reset (use title to distinguish from the time range button)
    const allButton = logViewer.getByTitle("All streams");
    await allButton.click();
    await expect(allButton).toHaveAttribute("aria-pressed", "true");
  });
});

test.describe("Log Viewer (task detail)", () => {
  test("Logs section is present on task detail page", async ({ page }) => {
    await navigateToFirst(page, "/tasks", /\/tasks\/.+/);

    await expect(page.getByRole("button", { name: "Logs", exact: true })).toBeVisible({
      timeout: 10_000,
    });
  });
});
