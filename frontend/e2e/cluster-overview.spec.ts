import { test, expect } from "./fixtures";

test.describe("Cluster Overview", () => {
  test("page loads with Cluster Overview heading", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Cluster Overview" })).toBeVisible();
  });

  test("4 health cards are visible", async ({ page }) => {
    await page.goto("/");

    // Wait for snapshot to load (cards switch from skeleton to real content)
    await expect(page.getByRole("link", { name: /Nodes/i }).first()).toBeVisible({ timeout: 10_000 });

    for (const label of ["Nodes", "Services", "Failed Tasks", "Tasks"]) {
      await expect(page.getByRole("link", { name: new RegExp(label, "i") }).first()).toBeVisible();
    }
  });

  test("Nodes card links to /nodes", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("link", { name: /Nodes/i }).first()).toBeVisible({ timeout: 10_000 });
    await page.getByRole("link", { name: /Nodes/i }).first().click();
    await expect(page).toHaveURL("/nodes");
  });

  test("Services card links to /services", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("link", { name: /Services/i }).first()).toBeVisible({ timeout: 10_000 });
    await page.getByRole("link", { name: /Services/i }).first().click();
    await expect(page).toHaveURL("/services");
  });

  test("Tasks card links to /tasks", async ({ page }) => {
    await page.goto("/");
    // Both "Failed Tasks" and "Tasks" link to /tasks; pick the "Tasks" card (last)
    const cards = page.getByRole("link", { name: /^Tasks$/i });
    await expect(cards.first()).toBeVisible({ timeout: 10_000 });
    await cards.first().click();
    await expect(page).toHaveURL("/tasks");
  });

  test("Capacity section is collapsible", async ({ page }) => {
    // Clear any persisted state so the section starts open
    await page.goto("/");
    await page.evaluate(() => localStorage.removeItem("section:capacity"));
    await page.reload();

    const toggle = page.getByRole("button", { name: /Capacity/i });
    await expect(toggle).toBeVisible({ timeout: 10_000 });

    // Section should be open by default — CPU bar is a button inside the capacity section
    const cpuBar = page.getByRole("button", { name: /CPU/i }).first();
    await expect(cpuBar).toBeVisible({ timeout: 10_000 });

    // Collapse
    await toggle.click();
    await expect(cpuBar).not.toBeVisible();

    // Expand again
    await toggle.click();
    await expect(cpuBar).toBeVisible();
  });

  test("Recent Activity section is collapsible", async ({ page }) => {
    await page.goto("/");
    await page.evaluate(() => localStorage.removeItem("section:recent-activity"));
    await page.reload();

    const toggle = page.getByRole("button", { name: /Recent Activity/i });
    await expect(toggle).toBeVisible({ timeout: 10_000 });

    // Collapse
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "false");

    // Expand again
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
  });

  test("monitoring banner shows Dismiss button when partially configured and dismissing hides it", async ({
    page,
    monitoring,
  }) => {
    // This test only makes sense when Prometheus is NOT fully healthy
    // (either unconfigured or partial). Skip when everything is green.
    const fullyHealthy =
      monitoring?.prometheus && monitoring?.nodeExporter && monitoring?.cadvisor;
    test.skip(!!fullyHealthy, "Monitoring is fully healthy — no banner expected");

    // Ensure the dismiss flag is cleared so the banner is visible
    await page.goto("/");
    await page.evaluate(() => localStorage.removeItem("cetacean:dismiss-monitoring-banner"));
    await page.reload();

    const dismissButton = page.getByRole("button", { name: "Dismiss" });
    await expect(dismissButton).toBeVisible({ timeout: 10_000 });

    await dismissButton.click();
    await expect(dismissButton).not.toBeVisible();
  });

  test("Recommendations summary View all link navigates to /recommendations", async ({ page }) => {
    await page.goto("/");

    // The "View all" link is only rendered when there are recommendations.
    const viewAll = page.getByRole("link", { name: /View all/i });

    try {
      await expect(viewAll).toBeVisible({ timeout: 5_000 });
    } catch {
      test.skip(true, "No recommendations present — View all link not rendered");
    }

    await viewAll.click();
    await expect(page).toHaveURL("/recommendations");
  });

  test.describe("Resource Usage by Stack (Prometheus)", () => {
    test("range picker buttons are visible", async ({ page, monitoring }) => {
      test.skip(!monitoring?.prometheus, "Prometheus not available");

      await page.goto("/");

      // Wait for the MetricsPanel section to appear
      const section = page.getByRole("button", { name: /Resource Usage by Stack/i });
      await expect(section).toBeVisible({ timeout: 15_000 });

      for (const label of ["1H", "6H", "24H", "7D"]) {
        await expect(page.getByRole("button", { name: label })).toBeVisible();
      }
    });

    test("stacked area toggle is visible", async ({ page, monitoring }) => {
      test.skip(!monitoring?.prometheus, "Prometheus not available");

      await page.goto("/");

      const section = page.getByRole("button", { name: /Resource Usage by Stack/i });
      await expect(section).toBeVisible({ timeout: 15_000 });

      // The stacked area toggle has a title attribute
      const stackToggle = page.getByTitle(/Switch to (stacked area|line chart)/i);
      await expect(stackToggle).toBeVisible();
    });

    test("pause/resume button is visible", async ({ page, monitoring }) => {
      test.skip(!monitoring?.prometheus, "Prometheus not available");

      await page.goto("/");

      const section = page.getByRole("button", { name: /Resource Usage by Stack/i });
      await expect(section).toBeVisible({ timeout: 15_000 });

      const pauseResume = page.getByTitle(/Pause live streaming|Resume live streaming/i);
      await expect(pauseResume).toBeVisible();
    });
  });
});
