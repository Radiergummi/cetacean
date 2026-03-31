import { test, expect } from "./fixtures";

test.describe("Navigation Bar", () => {
  test("logo links to /", async ({ page }) => {
    await page.goto("/nodes");
    await page.getByRole("link", { name: "Cetacean" }).click();
    await expect(page).toHaveURL("/");
  });

  test("all nav links navigate correctly", async ({ page }) => {
    await page.goto("/");

    const links: [string, string][] = [
      ["Nodes", "/nodes"],
      ["Stacks", "/stacks"],
      ["Services", "/services"],
      ["Tasks", "/tasks"],
      ["Configs", "/configs"],
      ["Secrets", "/secrets"],
      ["Networks", "/networks"],
      ["Volumes", "/volumes"],
      ["Swarm", "/swarm"],
      ["Topology", "/topology"],
      ["Metrics", "/metrics"],
    ];

    /* eslint-disable no-await-in-loop */
    for (const [label, path] of links) {
      await page.getByRole("link", { name: label, exact: true }).first().click();
      await expect(page).toHaveURL(path);
    }
    /* eslint-enable no-await-in-loop */
  });

  test("connection status shows Live", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("status").getByText("Live")).toBeVisible();
  });

  test("theme toggle changes theme", async ({ page }) => {
    await page.goto("/");
    const button = page.getByRole("button", { name: /Theme/ });
    await expect(button).toBeVisible();

    const initialLabel = await button.getAttribute("aria-label");
    await button.click();
    const afterLabel = await button.getAttribute("aria-label");
    expect(afterLabel).not.toBe(initialLabel);
  });

  test("recommendations button navigates to /recommendations", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: "Recommendations" }).click();
    await expect(page).toHaveURL("/recommendations");
  });
});

test.describe("Keyboard Shortcuts", () => {
  test("? opens and closes shortcuts overlay", async ({ page }) => {
    await page.goto("/");
    await page.locator("body").click();

    await page.keyboard.press("?");
    await expect(page.getByRole("heading", { name: "Keyboard Shortcuts" })).toBeVisible();

    await page.keyboard.press("?");
    await expect(page.getByRole("heading", { name: "Keyboard Shortcuts" })).not.toBeVisible();
  });

  test("navigation chords", async ({ page }) => {
    const chords: [string, string][] = [
      ["h", "/"],
      ["n", "/nodes"],
      ["s", "/services"],
      ["a", "/tasks"],
      ["k", "/stacks"],
      ["c", "/configs"],
      ["x", "/secrets"],
      ["w", "/networks"],
      ["v", "/volumes"],
      ["i", "/swarm"],
      ["t", "/topology"],
      ["r", "/recommendations"],
      ["m", "/metrics"],
    ];

    /* eslint-disable no-await-in-loop */
    for (const [key, path] of chords) {
      const startPage = path === "/nodes" ? "/services" : "/nodes";
      await page.goto(startPage);
      await page.locator("body").click();

      await page.keyboard.press("g");
      await page.keyboard.press(key);
      await expect(page).toHaveURL(path);
    }
    /* eslint-enable no-await-in-loop */
  });

  test("j/k navigate list rows and Enter opens", async ({ page }) => {
    await page.goto("/nodes");

    const firstRow = page.locator("table tbody tr").first();
    await expect(firstRow).toBeVisible({ timeout: 10_000 });

    await page.locator("main").click();

    await page.keyboard.press("j");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/\/nodes\/.+/);
  });
});

test.describe("Search", () => {
  /**
   * Helper: open the search palette by clicking the search button.
   * More reliable than keyboard shortcuts for tests that depend on
   * the palette being open.
   */
  async function openPalette(page: import("@playwright/test").Page) {
    // The search button contains "Search..." text on wide viewports,
    // or just a search icon on narrow ones. Use the visible button.
    await page.locator("button:has(svg)", { hasText: /Search/ }).click();
    const dialog = page.getByRole("dialog", { name: "Search" });
    await expect(dialog).toBeVisible();
    return dialog;
  }

  test("/ opens search palette and focuses input", async ({ page }) => {
    await page.goto("/");
    await page.locator("body").click();

    await page.keyboard.press("/");

    const dialog = page.getByRole("dialog", { name: "Search" });
    await expect(dialog).toBeVisible();
    await expect(dialog.locator("input")).toBeFocused();
  });

  test("Cmd+K opens search palette", async ({ page }) => {
    await page.goto("/");

    await page.keyboard.press(`${process.platform === "darwin" ? "Meta" : "Control"}+k`);

    const dialog = page.getByRole("dialog", { name: "Search" });
    await expect(dialog).toBeVisible();
  });

  test("search palette shows grouped results", async ({ page }) => {
    await page.goto("/");

    const dialog = await openPalette(page);

    const input = dialog.locator("input");
    await input.fill("ingress");

    // Wait for at least one group header to appear
    await expect(dialog.locator("section header").first()).toBeVisible({ timeout: 5_000 });
  });

  test("Esc closes palette", async ({ page }) => {
    await page.goto("/");

    const dialog = await openPalette(page);

    await page.keyboard.press("Escape");
    await expect(dialog).not.toBeVisible();
  });
});
