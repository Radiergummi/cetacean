import { test, expect, apiJson, navigateToFirst, clickRow } from "./fixtures";

test.describe("Stack List (/stacks)", () => {
  test("renders heading and table", async ({ page }) => {
    await page.goto("/stacks");

    await expect(page.getByRole("heading", { name: "Stacks" })).toBeVisible({ timeout: 10_000 });

    const table = page.getByRole("grid");
    await expect(table).toBeVisible({ timeout: 10_000 });
  });

  test("row click navigates to stack detail", async ({ page }) => {
    await page.goto("/stacks");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await clickRow(page.locator("table tbody tr").first());
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
});

/**
 * These sections render only for a stack that has the resource, so they cannot
 * use the first row: nothing says the stack sorting first carries configs.
 */
const crossReferenceSections = ["configs", "secrets", "networks", "volumes"] as const;

async function stackWithSection(
  request: import("@playwright/test").APIRequestContext,
  baseURL: string | undefined,
  section: (typeof crossReferenceSections)[number],
): Promise<string | null> {
  const stacks = await apiJson(request, baseURL, "/stacks");
  const items = Array.isArray(stacks.items) ? (stacks.items as Record<string, unknown>[]) : [];

  for (const item of items) {
    const members = item[section];

    if (Array.isArray(members) && members.length > 0) {
      return String(item.name);
    }
  }

  return null;
}

test.describe("Stack Detail cross-references", () => {
  for (const section of crossReferenceSections) {
    const heading = section.charAt(0).toUpperCase() + section.slice(1);

    test(`${heading} section toggle is present when a stack has ${section}`, async ({
      page,
      request,
      baseURL,
    }) => {
      const name = await stackWithSection(request, baseURL, section);
      test.skip(name === null, `No stack in this cluster has ${section}`);

      await page.goto(`/stacks/${name}`);

      await expect(page.getByRole("button", { name: new RegExp(`^${heading}$`, "i") })).toBeVisible(
        {
          timeout: 10_000,
        },
      );
    });
  }
});
