import { test, expect, writesEnabled } from "./fixtures";
import type { Page } from "@playwright/test";

async function navigateToFirstService(page: Page) {
  await page.goto("/services");
  await page.waitForSelector("table tbody tr");
  await page.locator("table tbody tr").first().click();
  await page.waitForURL(/\/services\//);
}

test.describe("Service Editors", () => {
  test.skip(!writesEnabled, "Write operations disabled (set CETACEAN_E2E_WRITE=1)");

  test.beforeEach(async ({ page }) => {
    await navigateToFirstService(page);
    // Wait for page to fully load before each test
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });
  });

  test("environment variables: Edit button opens edit mode", async ({ page }) => {
    const envSection = page.getByRole("button", { name: /Environment Variables/i });
    const count = await envSection.count();

    if (count === 0) {
      test.skip();
      return;
    }

    // Ensure the section is open
    const isExpanded = await envSection.getAttribute("aria-expanded");

    if (isExpanded === "false") {
      await envSection.click();
    }

    const editButton = page.getByRole("button", { name: /^Edit$/i }).first();
    await expect(editButton).toBeVisible({ timeout: 5_000 });
    await editButton.click();

    // Edit mode shows Save and Cancel buttons
    await expect(page.getByRole("button", { name: /^Save$/i }).first()).toBeVisible({
      timeout: 5_000,
    });
    await expect(page.getByRole("button", { name: /^Cancel$/i }).first()).toBeVisible();
  });

  test("environment variables: Cancel discards edit mode", async ({ page }) => {
    const envSection = page.getByRole("button", { name: /Environment Variables/i });
    const count = await envSection.count();

    if (count === 0) {
      test.skip();
      return;
    }

    const isExpanded = await envSection.getAttribute("aria-expanded");

    if (isExpanded === "false") {
      await envSection.click();
    }

    const editButton = page.getByRole("button", { name: /^Edit$/i }).first();
    await expect(editButton).toBeVisible({ timeout: 5_000 });
    await editButton.click();

    // In edit mode
    await expect(page.getByRole("button", { name: /^Cancel$/i }).first()).toBeVisible();

    // Click Cancel
    await page.getByRole("button", { name: /^Cancel$/i }).first().click();

    // Edit mode is dismissed — Save button is gone, Edit button is back
    await expect(page.getByRole("button", { name: /^Save$/i })).not.toBeVisible();
    await expect(page.getByRole("button", { name: /^Edit$/i }).first()).toBeVisible();
  });

  test("environment variables: Escape cancels edit mode", async ({ page }) => {
    const envSection = page.getByRole("button", { name: /Environment Variables/i });
    const count = await envSection.count();

    if (count === 0) {
      test.skip();
      return;
    }

    const isExpanded = await envSection.getAttribute("aria-expanded");

    if (isExpanded === "false") {
      await envSection.click();
    }

    const editButton = page.getByRole("button", { name: /^Edit$/i }).first();
    await expect(editButton).toBeVisible({ timeout: 5_000 });
    await editButton.click();

    await expect(page.getByRole("button", { name: /^Cancel$/i }).first()).toBeVisible();

    // Press Escape to cancel
    await page.keyboard.press("Escape");

    // Edit mode is dismissed
    await expect(page.getByRole("button", { name: /^Save$/i })).not.toBeVisible();
    await expect(page.getByRole("button", { name: /^Edit$/i }).first()).toBeVisible();
  });

  test("labels: Edit button shows Save and Cancel", async ({ page }) => {
    const labelsSection = page.getByRole("button", { name: /^Labels$/i });
    const count = await labelsSection.count();

    if (count === 0) {
      test.skip();
      return;
    }

    const isExpanded = await labelsSection.getAttribute("aria-expanded");

    if (isExpanded === "false") {
      await labelsSection.click();
    }

    const editButton = page.getByRole("button", { name: /^Edit$/i }).first();
    await expect(editButton).toBeVisible({ timeout: 5_000 });
    await editButton.click();

    await expect(page.getByRole("button", { name: /^Save$/i }).first()).toBeVisible({
      timeout: 5_000,
    });
    await expect(page.getByRole("button", { name: /^Cancel$/i }).first()).toBeVisible();
  });

  test("labels: Cancel returns to read mode", async ({ page }) => {
    const labelsSection = page.getByRole("button", { name: /^Labels$/i });
    const count = await labelsSection.count();

    if (count === 0) {
      test.skip();
      return;
    }

    const isExpanded = await labelsSection.getAttribute("aria-expanded");

    if (isExpanded === "false") {
      await labelsSection.click();
    }

    const editButton = page.getByRole("button", { name: /^Edit$/i }).first();
    await expect(editButton).toBeVisible({ timeout: 5_000 });
    await editButton.click();

    await expect(page.getByRole("button", { name: /^Cancel$/i }).first()).toBeVisible();
    await page.getByRole("button", { name: /^Cancel$/i }).first().click();

    await expect(page.getByRole("button", { name: /^Save$/i })).not.toBeVisible();
    await expect(page.getByRole("button", { name: /^Edit$/i }).first()).toBeVisible();
  });

  test("deploy configuration: section expands and sub-section Edit buttons are present", async ({
    page,
  }) => {
    // Clear persisted state to ensure closed default
    await page.evaluate(() => localStorage.removeItem("section:deploy-configuration"));
    await page.reload();
    await expect(page).toHaveURL(/\/services\//);
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });

    const deployToggle = page.getByRole("button", { name: /Deploy Configuration/i });
    await expect(deployToggle).toBeVisible({ timeout: 10_000 });

    // Expand if not already open
    const isExpanded = await deployToggle.getAttribute("aria-expanded");

    if (isExpanded === "false") {
      await deployToggle.click();
    }

    await expect(deployToggle).toHaveAttribute("aria-expanded", "true");

    // At least one Edit button should appear inside the Deploy Configuration section.
    // We check that an Edit button is visible anywhere on the page now that the section is open.
    const editButtons = page.getByRole("button", { name: /^Edit$/i });
    const editCount = await editButtons.count();
    expect(editCount).toBeGreaterThan(0);
  });
});
