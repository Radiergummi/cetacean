import { test, expect } from "./fixtures";

test.describe("Topology (/topology)", () => {
  test("renders Topology heading", async ({ page }) => {
    await page.goto("/topology");
    await expect(page.getByRole("heading", { name: "Topology" })).toBeVisible({ timeout: 10_000 });
  });

  test("logical view: ReactFlow canvas is visible", async ({ page }) => {
    await page.goto("/topology");
    await expect(page.getByRole("heading", { name: "Topology" })).toBeVisible({ timeout: 10_000 });
    // ReactFlow renders a div.react-flow — no semantic selector available
    await expect(page.locator(".react-flow")).toBeVisible({ timeout: 10_000 });
  });

  test("physical view: switching segmented control keeps canvas visible", async ({ page }) => {
    await page.goto("/topology");
    await expect(page.getByRole("heading", { name: "Topology" })).toBeVisible({ timeout: 10_000 });

    const physicalButton = page.getByRole("button", { name: "Physical" });
    await expect(physicalButton).toBeVisible({ timeout: 10_000 });
    await physicalButton.click();

    // ReactFlow renders a div.react-flow — no semantic selector available
    await expect(page.locator(".react-flow")).toBeVisible({ timeout: 10_000 });
  });
});
