import { test, expect, writesEnabled, navigateToFirst } from "./fixtures";
import type { Page } from "@playwright/test";

test.describe("Service Editors", () => {
  test.skip(!writesEnabled, "Write operations disabled (set CETACEAN_E2E_WRITE=1)");

  test.beforeEach(async ({ page }) => {
    await navigateToFirst(page, "/services", /\/services\/.+/);
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });
  });

  /**
   * Ensure a collapsible section is present and expanded.
   * Skips the test if the section doesn't exist on this service.
   */
  async function ensureSectionOpen(page: Page, name: RegExp) {
    const toggle = page.getByRole("button", { name });
    const count = await toggle.count();
    test.skip(count === 0, "Section not present on this service");

    if ((await toggle.getAttribute("aria-expanded")) === "false") {
      await toggle.click();
    }
  }

  /** Click the first Edit button visible on the page. */
  async function clickEdit(page: Page) {
    const editButton = page.getByRole("button", { name: /^Edit$/i }).first();
    await expect(editButton).toBeVisible({ timeout: 5_000 });
    await editButton.click();
  }

  test("environment variables: Edit button opens edit mode", async ({ page }) => {
    await ensureSectionOpen(page, /Environment Variables/i);
    await clickEdit(page);

    await expect(page.getByRole("button", { name: /^Save$/i }).first()).toBeVisible({
      timeout: 5_000,
    });
    await expect(page.getByRole("button", { name: /^Cancel$/i }).first()).toBeVisible();
  });

  test("environment variables: Cancel discards edit mode", async ({ page }) => {
    await ensureSectionOpen(page, /Environment Variables/i);
    await clickEdit(page);

    await page.getByRole("button", { name: /^Cancel$/i }).first().click();

    await expect(page.getByRole("button", { name: /^Save$/i })).not.toBeVisible();
    await expect(page.getByRole("button", { name: /^Edit$/i }).first()).toBeVisible();
  });

  test("environment variables: Escape cancels edit mode", async ({ page }) => {
    await ensureSectionOpen(page, /Environment Variables/i);
    await clickEdit(page);

    await page.keyboard.press("Escape");

    await expect(page.getByRole("button", { name: /^Save$/i })).not.toBeVisible();
    await expect(page.getByRole("button", { name: /^Edit$/i }).first()).toBeVisible();
  });

  test("labels: Edit button shows Save and Cancel", async ({ page }) => {
    await ensureSectionOpen(page, /^Labels$/i);
    await clickEdit(page);

    await expect(page.getByRole("button", { name: /^Save$/i }).first()).toBeVisible({
      timeout: 5_000,
    });
    await expect(page.getByRole("button", { name: /^Cancel$/i }).first()).toBeVisible();
  });

  test("labels: Cancel returns to read mode", async ({ page }) => {
    await ensureSectionOpen(page, /^Labels$/i);
    await clickEdit(page);

    await page.getByRole("button", { name: /^Cancel$/i }).first().click();

    await expect(page.getByRole("button", { name: /^Save$/i })).not.toBeVisible();
    await expect(page.getByRole("button", { name: /^Edit$/i }).first()).toBeVisible();
  });

  test("deploy configuration: section expands and sub-section Edit buttons are present", async ({
    page,
  }) => {
    await page.evaluate(() => localStorage.removeItem("section:deploy-configuration"));
    await page.reload();
    await expect(page).toHaveURL(/\/services\/.+/);
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });

    await ensureSectionOpen(page, /Deploy Configuration/i);

    const editButtons = page.getByRole("button", { name: /^Edit$/i });
    const editCount = await editButtons.count();
    expect(editCount).toBeGreaterThan(0);
  });
});
