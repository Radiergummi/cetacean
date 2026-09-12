import { test, expect, writesEnabled, navigateToFirst } from "./fixtures";
import type { Locator, Page } from "@playwright/test";

test.describe("Service Editors", () => {
  test.skip(!writesEnabled, "Write operations disabled (set CETACEAN_E2E_WRITE=1)");

  test.beforeEach(async ({ page }) => {
    await navigateToFirst(page, "/services", /\/services\/.+/);
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });
  });

  /**
   * Ensure a collapsible section is present and expanded, and return its
   * header row.
   *
   * The section toggle is matched as a disclosure control rather than by name
   * alone: a service detail page carries four buttons named "Labels", three of
   * them unrelated, and only the toggle has aria-expanded. An unqualified name
   * match is a strict-mode violation — which this file never hit, because the
   * whole of it is gated behind CETACEAN_E2E_WRITE and had not been run.
   */
  async function ensureSectionOpen(page: Page, name: RegExp) {
    const toggle = page.getByRole("button", { name }).and(page.locator("[aria-expanded]"));
    const count = await toggle.count();
    test.skip(count === 0, "Section not present on this service");

    if ((await toggle.getAttribute("aria-expanded")) === "false") {
      await toggle.click();
    }

    return toggle.locator("xpath=..");
  }

  /**
   * Click the Edit button belonging to one section.
   *
   * Scoped to that section's header, because a service detail page has an Edit
   * button per editable section and the first one in the DOM belongs to
   * whichever section happens to come first — not to the one under test.
   */
  async function clickEdit(header: Locator) {
    const editButton = header.getByRole("button", { name: /^Edit$/i });
    await expect(editButton).toBeVisible({ timeout: 5_000 });
    await editButton.click();
  }

  test("environment variables: Edit button opens edit mode", async ({ page }) => {
    const header = await ensureSectionOpen(page, /Environment Variables/i);
    await clickEdit(header);

    await expect(page.getByRole("button", { name: /^Save$/i }).first()).toBeVisible({
      timeout: 5_000,
    });
    await expect(page.getByRole("button", { name: /^Cancel$/i }).first()).toBeVisible();
  });

  test("environment variables: Cancel discards edit mode", async ({ page }) => {
    const header = await ensureSectionOpen(page, /Environment Variables/i);
    await clickEdit(header);

    await page
      .getByRole("button", { name: /^Cancel$/i })
      .first()
      .click();

    await expect(page.getByRole("button", { name: /^Save$/i })).not.toBeVisible();
    await expect(page.getByRole("button", { name: /^Edit$/i }).first()).toBeVisible();
  });

  test("environment variables: Escape cancels edit mode", async ({ page }) => {
    const header = await ensureSectionOpen(page, /Environment Variables/i);
    await clickEdit(header);

    await page.keyboard.press("Escape");

    await expect(page.getByRole("button", { name: /^Save$/i })).not.toBeVisible();
    await expect(page.getByRole("button", { name: /^Edit$/i }).first()).toBeVisible();
  });

  test("labels: Edit button shows Save and Cancel", async ({ page }) => {
    const header = await ensureSectionOpen(page, /^Labels$/i);
    await clickEdit(header);

    await expect(page.getByRole("button", { name: /^Save$/i }).first()).toBeVisible({
      timeout: 5_000,
    });
    await expect(page.getByRole("button", { name: /^Cancel$/i }).first()).toBeVisible();
  });

  test("labels: Cancel returns to read mode", async ({ page }) => {
    const header = await ensureSectionOpen(page, /^Labels$/i);
    await clickEdit(header);

    await page
      .getByRole("button", { name: /^Cancel$/i })
      .first()
      .click();

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
