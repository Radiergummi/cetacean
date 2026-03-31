import { test, expect } from "./fixtures";

test.describe("Swarm (/swarm)", () => {
  test("shows Swarm heading and cluster metadata", async ({ page }) => {
    await page.goto("/swarm");
    await expect(page.getByRole("heading", { name: "Swarm" })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Cluster ID")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Created")).toBeVisible();
  });

  test("join command buttons open dialogs with docker swarm join text", async ({ page }) => {
    await page.goto("/swarm");
    await expect(page.getByRole("heading", { name: "Swarm" })).toBeVisible({ timeout: 10_000 });

    const workerButton = page.getByRole("button", { name: /Join Worker/i });
    await expect(workerButton).toBeVisible({ timeout: 10_000 });
    await workerButton.click();

    await expect(page.getByText(/docker swarm join/)).toBeVisible({ timeout: 5_000 });
  });

  test("encryption section is visible", async ({ page }) => {
    await page.goto("/swarm");
    await expect(page.getByRole("heading", { name: "Swarm" })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Encryption")).toBeVisible({ timeout: 10_000 });
  });
});
