import { test, expect, writesEnabled } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * Outcome assertions for the dashboard's write paths. The rest of this suite
 * asserts affordances only — an Edit button opens edit mode, Cancel closes it
 * — and saves nothing, so each case here ends by reading the resource back
 * from the API and asserting the cluster changed. They mutate shared fixture
 * state, so they run serially and each reverts what it did, the revert
 * asserted too.
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
    // shop_lonely, not the first row, so the specs navigating to "the first
    // service" are not reading it while this one changes it. The list renders
    // stack and name as separate cells, so "lonely" is what distinguishes it.
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

  test("recommendation fix: applying the suggested value scales the service", async ({ page }) => {
    // The one-click fix a recommendation offers is a write like any other, and
    // the only one a user reaches without opening an editor at all. Nothing
    // had ever clicked it.
    const serviceId = await openService(page, "lonely");
    expect(await replicaCount(page, serviceId)).toBe(1);

    const apply = page.getByRole("button", { name: /Apply suggested value/i });
    await expect(apply).toBeVisible({ timeout: 20_000 });

    // The suggestion the button is about to apply, read from the page rather
    // than assumed: a case that hardcodes 2 still passes if the button applies
    // something else entirely.
    const context = await apply.locator("xpath=../..").innerText();
    const suggested = Number(/Suggested:\s*(\d+)\s*replicas/i.exec(context)?.[1]);
    expect(suggested).toBeGreaterThan(1);

    await apply.click();

    await expect
      .poll(async () => replicaCount(page, serviceId), { timeout: 30_000 })
      .toBe(suggested);

    const scaled = await page.request.put(`/services/${serviceId}/scale`, {
      data: { replicas: 1 },
    });
    expect(scaled.ok()).toBeTruthy();

    await expect.poll(async () => replicaCount(page, serviceId), { timeout: 30_000 }).toBe(1);
  });

  test("restart: the button replaces the service's tasks", async ({ page }) => {
    const serviceId = await openService(page, "lonely");

    const before = await runningTaskIDs(page, serviceId);
    expect(before.length).toBeGreaterThan(0);

    await page.getByRole("button", { name: /^Restart$/i }).click();

    // A destructive action confirms in an *alertdialog*, which is a distinct
    // ARIA role: getByRole("dialog") does not match one, so a case that looks
    // for a dialog here silently skips the confirmation and asserts against a
    // cluster nothing was ever asked to change.
    await confirmIn(page, /^Restart$/i);

    // A restart is a forced update: Docker starts a replacement task and winds
    // the old one down. The claim is that a task the service did not have
    // before is now running, not that the old one has gone — the two overlap
    // while the replacement comes up.
    await expect
      .poll(
        async () => {
          const now = await runningTaskIDs(page, serviceId);

          return now.some((id) => !before.includes(id));
        },
        { timeout: 60_000 },
      )
      .toBe(true);
  });

  test("image update and rollback: both reach the service spec", async ({ page }) => {
    // shop_flaky, deliberately: it crash-loops by design and never converges,
    // so pointing it at an image that cannot be pulled costs the fixture
    // cluster nothing that was not already true of it.
    const serviceId = await openService(page, "flaky");
    const original = await serviceImage(page, serviceId);
    expect(original).toBeTruthy();

    const replacement = "cetacean-e2e-fixture:rolled-forward";

    await page.getByTitle("Update image").click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible({ timeout: 10_000 });
    await dialog.locator("input").fill(replacement);
    await dialog.getByRole("button", { name: /^Update$/i }).click();
    await expect(dialog).not.toBeVisible({ timeout: 20_000 });

    await expect
      .poll(async () => serviceImage(page, serviceId), { timeout: 30_000 })
      .toContain(replacement);

    // Rollback is the only write whose correctness is defined by a previous
    // one: it restores the spec Docker kept, so it can only be tested after a
    // change has actually landed.
    await page.reload();
    await page.getByRole("button", { name: /^Rollback$/i }).click();
    await confirmIn(page, /^Rollback$/i);

    await expect
      .poll(async () => serviceImage(page, serviceId), { timeout: 30_000 })
      .toContain(original.split("@")[0]!);
  });
});

/** Confirm a destructive action in the alertdialog its trigger opens. */
async function confirmIn(page: Page, action: RegExp) {
  const confirm = page.getByRole("alertdialog");
  await expect(confirm).toBeVisible({ timeout: 10_000 });
  await confirm.getByRole("button", { name: action }).click();
  await expect(confirm).not.toBeVisible({ timeout: 20_000 });
}

/** Open a service's detail page by the part of its name that distinguishes it. */
async function openService(page: Page, name: string): Promise<string> {
  await page.goto("/services");

  const row = page.locator("table tbody tr").filter({ hasText: name }).first();
  await expect(row).toBeVisible({ timeout: 15_000 });
  await row.click();
  await page.waitForURL(/\/services\/.+/);

  return new URL(page.url()).pathname.split("/").pop()!;
}

/** The service's desired replica count, as Docker currently holds it. */
async function replicaCount(page: Page, id: string): Promise<number | undefined> {
  const response = await page.request.get(`/services/${id}`, {
    headers: { Accept: "application/json" },
  });
  const body = await response.json();

  return body.service?.Spec?.Mode?.Replicated?.Replicas;
}

/** The image in the service's spec, as Docker currently holds it. */
async function serviceImage(page: Page, id: string): Promise<string> {
  const response = await page.request.get(`/services/${id}`, {
    headers: { Accept: "application/json" },
  });
  const body = await response.json();

  return body.service?.Spec?.TaskTemplate?.ContainerSpec?.Image ?? "";
}

/** The IDs of the service's currently running tasks. */
async function runningTaskIDs(page: Page, serviceId: string): Promise<string[]> {
  const response = await page.request.get("/tasks?limit=200", {
    headers: { Accept: "application/json" },
  });
  const body = await response.json();

  return (body.items ?? [])
    .filter(
      (task: { ServiceID?: string; Status?: { State?: string } }) =>
        task.ServiceID === serviceId && task.Status?.State === "running",
    )
    .map((task: { ID: string }) => task.ID);
}

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
