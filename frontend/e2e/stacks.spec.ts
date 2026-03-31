import { test, expect, navigateToFirst } from "./fixtures";

test.describe("Stack List (/stacks)", () => {
  test("renders heading and table", async ({ page }) => {
    await page.goto("/stacks");

    await expect(page.getByRole("heading", { name: "Stacks" })).toBeVisible({ timeout: 10_000 });

    const table = page.getByRole("table");
    await expect(table).toBeVisible({ timeout: 10_000 });
  });

  test("row click navigates to stack detail", async ({ page }) => {
    await page.goto("/stacks");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/stacks\/.+/);
  });
});

test.describe("Stack Detail", () => {
  test.beforeEach(async ({ page }) => {
    await navigateToFirst(page, "/stacks", /\/stacks\/.+/);
  });

  test("shows stack name in heading", async ({ page }) => {
    await expect(page.getByRole("heading").first()).toBeVisible({ timeout: 10_000 });
  });

  test("Services section is present", async ({ page }) => {
    await expect(page.getByRole("button", { name: /^Services$/i })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("Services section is collapsible", async ({ page }) => {
    const toggle = page.getByRole("button", { name: /^Services$/i });
    await expect(toggle).toBeVisible({ timeout: 10_000 });

    // Section should be open by default — the services table should be visible
    await expect(toggle).toHaveAttribute("aria-expanded", "true");

    // Collapse
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "false");

    // Expand again
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
  });

  test("Configs section toggle is present when stack has configs", async ({ page }) => {
    // Configs section only renders when the stack has configs
    await expect(page.getByRole("button", { name: /^Services$/i })).toBeVisible({
      timeout: 10_000,
    });

    const configsButton = page.getByRole("button", { name: /^Configs$/i });
    const count = await configsButton.count();
    test.skip(count === 0, "Stack has no configs");
    await expect(configsButton).toBeVisible();
  });

  test("Secrets section toggle is present when stack has secrets", async ({ page }) => {
    await expect(page.getByRole("button", { name: /^Services$/i })).toBeVisible({
      timeout: 10_000,
    });

    const secretsButton = page.getByRole("button", { name: /^Secrets$/i });
    const count = await secretsButton.count();
    test.skip(count === 0, "Stack has no secrets");
    await expect(secretsButton).toBeVisible();
  });

  test("Networks section toggle is present when stack has networks", async ({ page }) => {
    await expect(page.getByRole("button", { name: /^Services$/i })).toBeVisible({
      timeout: 10_000,
    });

    const networksButton = page.getByRole("button", { name: /^Networks$/i });
    const count = await networksButton.count();
    test.skip(count === 0, "Stack has no networks");
    await expect(networksButton).toBeVisible();
  });

  test("Volumes section toggle is present when stack has volumes", async ({ page }) => {
    await expect(page.getByRole("button", { name: /^Services$/i })).toBeVisible({
      timeout: 10_000,
    });

    const volumesButton = page.getByRole("button", { name: /^Volumes$/i });
    const count = await volumesButton.count();
    test.skip(count === 0, "Stack has no volumes");
    await expect(volumesButton).toBeVisible();
  });
});
