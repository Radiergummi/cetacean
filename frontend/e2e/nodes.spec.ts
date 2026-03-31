import { test, expect, navigateToFirst } from "./fixtures";

test.describe("Node List (/nodes)", () => {
  test("renders table with expected columns", async ({ page }) => {
    await page.goto("/nodes");

    const table = page.getByRole("table");
    await expect(table).toBeVisible({ timeout: 10_000 });

    const header = page.getByRole("row").first();
    await expect(header.getByText("Hostname")).toBeVisible();
    await expect(header.getByText("Role")).toBeVisible();
    await expect(header.getByText("Availability")).toBeVisible();
    await expect(header.getByText("Status")).toBeVisible();
  });

  test("renders at least one node row", async ({ page }) => {
    await page.goto("/nodes");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });
  });

  test("row click navigates to node detail", async ({ page }) => {
    await page.goto("/nodes");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await page.locator("table tbody tr").first().click();
    await expect(page).toHaveURL(/\/nodes\/.+/);
  });

  test("search filters nodes", async ({ page }) => {
    await page.goto("/nodes");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    // Read the first row's hostname to use as a search term
    const firstHostname = await page.locator("table tbody tr td").first().innerText();
    const searchInput = page.getByPlaceholder("Search nodes…");
    await searchInput.fill(firstHostname);

    // Row with that hostname should remain visible
    await expect(page.getByRole("cell", { name: firstHostname })).toBeVisible({ timeout: 5_000 });

    // Searching for something that won't match should show empty state
    await searchInput.fill("zzznomatch");
    await expect(page.getByText(/No nodes match your search/)).toBeVisible({ timeout: 5_000 });
  });

  test("clicking column header adds sort param to URL", async ({ page }) => {
    await page.goto("/nodes");

    await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });

    await page.getByRole("columnheader", { name: /Role/ }).click();
    await expect(page).toHaveURL(/[?&]sort=/);
  });
});

test.describe("Node Detail (/nodes/:id)", () => {
  test.beforeEach(async ({ page }) => {
    await navigateToFirst(page, "/nodes", /\/nodes\/.+/);
  });

  test("shows role, availability, and status metadata", async ({ page }) => {
    // The MetadataGrid contains InfoCard/RoleEditor/StatusCard/AvailabilityEditor items.
    // These render labelled values — look for the label text.
    await expect(page.getByText("Role")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Availability")).toBeVisible();
    await expect(page.getByText("Status")).toBeVisible();
  });

  test("tasks table renders", async ({ page }) => {
    // TasksTable uses CollapsibleSection which renders the title as a <button>
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });
  });

  test("activity section renders when history exists", async ({ page }) => {
    // ActivitySection renders only when there are history entries — it returns null for an
    // empty list. Wait for the tasks section (which always renders) to confirm the page is
    // loaded, then check whether recent activity appears.
    await expect(page.getByRole("button", { name: /^Tasks$/i })).toBeVisible({ timeout: 10_000 });

    const activityButton = page.getByRole("button", { name: /Recent Activity/i });
    const count = await activityButton.count();
    test.skip(count === 0, "No activity history present for this node");
    await expect(activityButton).toBeVisible();
  });

  test("labels section renders", async ({ page }) => {
    // KeyValueEditor uses CollapsibleSection with the provided title "Labels"
    await expect(page.getByRole("button", { name: /^Labels$/i })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("remove button is present", async ({ page }) => {
    // NodeActions renders a Remove button (may be disabled if node is not down)
    await expect(page.getByRole("button", { name: /Remove/i })).toBeVisible({ timeout: 10_000 });
  });
});
