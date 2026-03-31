import { test, expect } from "./fixtures";

test.describe("SSE / Connection Status", () => {
  test("connection status shows Live on cluster overview", async ({ page }) => {
    await page.goto("/");

    // Wait for the page to load (health cards appear once snapshot is loaded)
    await expect(page.getByRole("link", { name: /Nodes/i }).first()).toBeVisible({
      timeout: 10_000,
    });

    // ConnectionStatus renders "Live" text when connected
    await expect(page.getByText("Live")).toBeVisible({ timeout: 10_000 });
  });

  test("service list loads without disconnection errors", async ({ page }) => {
    await page.goto("/services");

    // Table must render successfully
    await expect(page.getByRole("table")).toBeVisible({ timeout: 10_000 });

    // Connection status must show Live (no reconnecting state)
    await expect(page.getByText("Live")).toBeVisible({ timeout: 10_000 });

    // No error banner / fetch error should be present
    await expect(page.getByText(/failed to load|connection lost|disconnected/i)).not.toBeVisible();
  });
});
