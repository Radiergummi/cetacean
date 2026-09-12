import { test, expect, writesEnabled } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * Outcome assertions for the dashboard's write paths.
 *
 * The rest of this suite asserts affordances — that an Edit button opens edit
 * mode, that Cancel closes it. None of it saves anything, which is how a
 * control that could only ever fail survived from March to September: the spec
 * that covered `PUT /services/{id}/mode` checked the switch was on the page,
 * and the endpoint behind it answered an engine error on every Docker version
 * ever shipped.
 *
 * So each case here ends by reading the resource back from the API and
 * asserting the cluster changed. A page that renders the new value optimistically
 * and a cluster that accepted it are different claims, and only the second one
 * is what the user asked for.
 *
 * The cases mutate shared fixture state, so they run serially and each reverts
 * what it did — the revert asserted as well, since a test that leaves a cluster
 * dirty is one the next run inherits.
 */
test.describe("Write outcomes", () => {
  test.skip(!writesEnabled, "Write operations disabled (set CETACEAN_E2E_WRITE=1)");
  // Longer than the suite default: these wait for Docker to converge, not for
  // a page to render.
  test.describe.configure({ mode: "serial", timeout: 90_000 });

  /** Expand a collapsible section if it is closed. */
  async function openSection(page: Page, name: RegExp) {
    const toggle = page.getByRole("button", { name });
    await expect(toggle).toBeVisible({ timeout: 15_000 });

    if ((await toggle.getAttribute("aria-expanded")) === "false") {
      await toggle.click();
    }

    return toggle;
  }

  test("node labels: saving writes the label to the cluster", async ({ page }) => {
    await page.goto("/nodes");
    await page.locator("table tbody tr").first().click();
    await page.waitForURL(/\/nodes\/.+/);

    const nodeId = new URL(page.url()).pathname.split("/").pop()!;
    const key = "cetacean.e2e.outcome";
    const value = `probe-${Date.now()}`;

    await openSection(page, /^Labels$/i);
    await page
      .getByRole("button", { name: /^Edit$/i })
      .first()
      .click();

    // An editor opened on a section that already has entries shows only those
    // rows; the blank one is what "Add another" adds. Asking for it only when
    // it is missing keeps this case working whether or not the node arrives
    // with labels.
    const newKey = page.getByPlaceholder("com.example.my-label");

    if ((await newKey.count()) === 0) {
      await page.getByRole("button", { name: /^Add another$/i }).click();
    }

    await newKey.last().fill(key);
    await page.getByPlaceholder("value").last().fill(value);
    await page
      .getByRole("button", { name: /^Save$/i })
      .first()
      .click();

    // The page settles back into read mode, which is the only thing the rest
    // of this suite would have checked.
    await expect(page.getByRole("button", { name: /^Save$/i })).not.toBeVisible({
      timeout: 15_000,
    });

    // The claim that matters: Docker holds the label.
    await expect
      .poll(async () => (await nodeLabels(page, nodeId))[key], { timeout: 15_000 })
      .toBe(value);

    // And the page is showing what the cluster holds, rather than only what
    // was typed into it.
    await page.reload();
    await openSection(page, /^Labels$/i);
    await expect(page.getByText(key, { exact: true })).toBeVisible({ timeout: 15_000 });

    // Revert, and prove the revert took: a case that dirties the fixture
    // cluster hands its mess to the next run.
    const removed = await page.request.patch(`/nodes/${nodeId}/labels`, {
      headers: { "Content-Type": "application/merge-patch+json" },
      data: { [key]: null },
    });
    expect(removed.ok()).toBeTruthy();

    await expect
      .poll(async () => key in (await nodeLabels(page, nodeId)), { timeout: 15_000 })
      .toBe(false);
  });

  test("service environment: saving writes the variable to the service spec", async ({ page }) => {
    // shop_lonely, not the first row: the specs that navigate to "the first
    // service" cannot be reading it while this one changes it underneath them.
    // The list renders a service's stack and name as separate cells, so a row
    // never holds the Docker name "shop_lonely" as a substring — "lonely" is
    // the part that distinguishes it from the other three.
    await page.goto("/services");

    const row = page.locator("table tbody tr").filter({ hasText: "lonely" }).first();
    await expect(row).toBeVisible({ timeout: 15_000 });
    await row.click();
    await page.waitForURL(/\/services\/.+/);

    const serviceId = new URL(page.url()).pathname.split("/").pop()!;
    const name = "CETACEAN_E2E_OUTCOME";
    const value = `probe-${Date.now()}`;

    // Scoped to this section's own header: a service detail page carries an
    // Edit button per editable section, and an unscoped one edits whichever
    // happens to come first in the DOM.
    const header = (await openSection(page, /^Environment Variables$/i)).locator("xpath=..");

    await header.getByRole("button", { name: /^Edit$/i }).click();

    const section = header.locator("xpath=..");
    const inputs = section.locator("input:visible");

    await inputs.nth(-2).fill(name);
    await inputs.nth(-1).fill(value);

    // Save sits in the section body rather than its header: the header only
    // carries controls while the section is *not* being edited.
    await section.getByRole("button", { name: /^Save$/i }).click();

    await expect(section.getByRole("button", { name: /^Save$/i })).not.toBeVisible({
      timeout: 20_000,
    });

    // The claim that matters: Docker holds the variable.
    await expect
      .poll(async () => (await serviceEnv(page, serviceId))[name], { timeout: 20_000 })
      .toBe(value);

    // Revert, and prove the revert took — a case that dirties the fixture
    // cluster hands its mess to the next run.
    const removed = await page.request.patch(`/services/${serviceId}/env`, {
      headers: { "Content-Type": "application/merge-patch+json" },
      data: { [name]: null },
    });
    expect(removed.ok()).toBeTruthy();

    await expect
      .poll(async () => name in (await serviceEnv(page, serviceId)), { timeout: 20_000 })
      .toBe(false);
  });
});

/** The service's environment, as Docker currently holds it. */
async function serviceEnv(page: Page, id: string): Promise<Record<string, string>> {
  const response = await page.request.get(`/services/${id}/env`, {
    headers: { Accept: "application/json" },
  });

  if (!response.ok()) {
    return {};
  }

  const body = await response.json();
  const env = body.env ?? body;

  return typeof env === "object" && env !== null ? (env as Record<string, string>) : {};
}

/** The node's labels, as Docker currently holds them. */
async function nodeLabels(page: Page, id: string): Promise<Record<string, string>> {
  const response = await page.request.get(`/nodes/${id}`, {
    headers: { Accept: "application/json" },
  });
  const body = await response.json();

  return body.node?.Spec?.Labels ?? {};
}
