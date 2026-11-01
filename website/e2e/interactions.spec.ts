import { expect, test } from "@playwright/test";

test.use({ viewport: { width: 375, height: 800 } });

test("search opens from the header on a tap", async ({ page }) => {
  await page.goto("/getting-started");
  await page.locator("#search-trigger-mobile").tap();

  const dialog = page.getByRole("dialog", { name: "Search documentation" });

  await expect(dialog).toBeInViewport();
  await expect(dialog.getByRole("textbox", { name: "Search" })).toBeFocused();

  await page.keyboard.type("operations level");
  await expect(dialog.locator("[data-search-item]").first()).toBeVisible();
});

test("the menu opens and closes on a tap", async ({ page }) => {
  await page.goto("/getting-started");

  const toggle = page.locator("#mobile-menu-toggle");
  const sidebar = page.locator("#mobile-sidebar");

  await toggle.tap();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await expect(sidebar.getByRole("link", { name: "Configuration" })).toBeInViewport();

  await page.locator("#mobile-sidebar-backdrop").tap({ position: { x: 10, y: 400 } });
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
});

test("a code tab shows its panel on a tap", async ({ page }) => {
  await page.goto("/api");

  const tabs = page.locator(".code-tabs").first();
  const second = tabs.getByRole("tab").nth(1);

  await second.tap();
  await expect(second).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(`#${await second.getAttribute("aria-controls")}`)).toBeVisible();
});
